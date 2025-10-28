package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/couchbase/gocb/v2"
	sharedconfig "github.com/paulcapestany/nostr_shared/config"
	sharedcouchbase "github.com/paulcapestany/nostr_shared/couchbase"
)

const (
	defaultConnStr  = "localhost"
	defaultBucket   = "all-nostr-events"
	defaultUsername = "admin"
	defaultPassword = "ore8airman7goods6feudal8mantle"
)

type fileConfig struct {
	ConnectionString string `json:"connectionString"`
	BucketName       string `json:"bucketName"`
	Username         string `json:"username"`
	Password         string `json:"password"`
}

// Custom flag to collect filenames
var filenames string

func init() {
	flag.StringVar(&filenames, "f", "", "Space-separated list of files to process")
}

// Global variable to track readiness state
var isReady bool = false

// Health check function for liveness probe
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK")
}

// Readiness check function for readiness probe
func readyzHandler(w http.ResponseWriter, r *http.Request) {
	if isReady {
		fmt.Fprintf(w, "Ready")
	} else {
		http.Error(w, "Not Ready", http.StatusServiceUnavailable)
	}
}

func startHealthServer() {
	http.HandleFunc("/healthz", healthzHandler)
	http.HandleFunc("/readyz", readyzHandler)

	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			log.Fatalf("Health check server failed: %v", err)
		}
	}()
}

func loadFileConfig(path string) (fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, err
	}

	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, err
	}
	return cfg, nil
}

func applyConfigOverrides(cfg fileConfig) string {
	if cfg.ConnectionString != "" {
		os.Setenv("COUCHBASE_CONNSTR", cfg.ConnectionString)
	}
	if cfg.Username != "" {
		os.Setenv("COUCHBASE_USER", cfg.Username)
	}
	if cfg.Password != "" {
		os.Setenv("COUCHBASE_PASSWORD", cfg.Password)
	}
	return cfg.BucketName
}

func applyFlagOverrides(conn, user, password string) {
	if conn != "" {
		os.Setenv("COUCHBASE_CONNSTR", conn)
	}
	if user != "" {
		os.Setenv("COUCHBASE_USER", user)
	}
	if password != "" {
		os.Setenv("COUCHBASE_PASSWORD", password)
	}
}

func ensureEnvDefaults() {
	if os.Getenv("COUCHBASE_CONNSTR") == "" {
		os.Setenv("COUCHBASE_CONNSTR", defaultConnStr)
	}
	if os.Getenv("COUCHBASE_USER") == "" {
		os.Setenv("COUCHBASE_USER", defaultUsername)
	}
	if os.Getenv("COUCHBASE_PASSWORD") == "" {
		os.Setenv("COUCHBASE_PASSWORD", defaultPassword)
	}
}

func resolveBucket(flagValue, fileValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if fileValue != "" {
		return fileValue
	}
	if envBucket := os.Getenv("COUCHBASE_BUCKET"); envBucket != "" {
		return envBucket
	}
	return ""
}

func main() {
	// Start the health and readiness server in the background
	startHealthServer()

	var configFile string
	flag.StringVar(&configFile, "config", "", "Configuration file path")
	connOverride := flag.String("conn", "", "Override Couchbase connection string")
	bucketOverride := flag.String("bucket", "", "Bucket name to write to")
	userOverride := flag.String("user", "", "Override Couchbase username")
	passOverride := flag.String("pass", "", "Override Couchbase password")
	defaultLogging := flag.Bool("v", false, "Verbose logging from 'gocb'")
	flag.Parse()

	fileBucketName := ""
	if configFile != "" {
		cfg, err := loadFileConfig(configFile)
		if err != nil {
			log.Fatalf("Error parsing config file: %v", err)
		}
		fileBucketName = applyConfigOverrides(cfg)
	}

	applyFlagOverrides(*connOverride, *userOverride, *passOverride)
	ensureEnvDefaults()

	sharedconfig.Setup()

	if *defaultLogging {
		gocb.SetLogger(gocb.DefaultStdioLogger())
	}

	targetBucket := resolveBucket(*bucketOverride, fileBucketName)

	cluster, sharedBucket, sharedCollection, _, err := sharedcouchbase.InitializeCouchbase()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := cluster.Close(nil); err != nil {
			log.Printf("Error closing Couchbase cluster: %v", err)
		}
	}()

	bucket := sharedBucket
	collection := sharedCollection

	if targetBucket == "" {
		targetBucket = bucket.Name()
	}
	if targetBucket == "" {
		targetBucket = defaultBucket
	}

	if targetBucket != bucket.Name() {
		bucket = cluster.Bucket(targetBucket)
		if err := bucket.WaitUntilReady(5*time.Second, nil); err != nil {
			log.Fatalf("Failed to wait for bucket %s: %v", targetBucket, err)
		}
		collection = bucket.DefaultCollection()
	}

	// Mark the application as ready after initialization completes
	isReady = true

	// Create a context that will be cancelled on interrupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal catching for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal in a separate goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case sig := <-sigChan:
			log.Printf("Received signal: %s, shutting down gracefully...", sig)
			cancel() // Cancel the context to signal shutdown
		case <-ctx.Done():
			// Context cancelled, no signal received
		}
	}()

	if filenames == "" {
		processFile(ctx, os.Stdin, collection)
	} else {
		filesToProcess := strings.Fields(filenames)
		for _, filename := range filesToProcess {
			file, err := os.Open(filename)
			if err != nil {
				log.Fatalf("Cannot open file %s: %v", filename, err)
			}

			processFile(ctx, file, collection)

			if err := file.Close(); err != nil {
				log.Printf("Error closing file %s: %v", filename, err)
			}
		}
	}

	// Wait for graceful shutdown completion
	wg.Wait()
	log.Println("Service shut down successfully.")
}

func processFile(ctx context.Context, file *os.File, col *gocb.Collection) {
	scanner := bufio.NewScanner(file)
	const maxBufferSize = 10 * 1024 * 1024      // Adjust the size as needed, e.g., 10MB
	buffer := make([]byte, 4096, maxBufferSize) // Initial size of 4KB, max of 10MB
	scanner.Buffer(buffer, maxBufferSize)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			log.Println("Processing interrupted, shutting down...")
			return
		default:
			processLine(scanner.Text(), col)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading from file: %v", err)
	}
}

func processLine(jsonInput string, col *gocb.Collection) {
	var document interface{}
	err := json.Unmarshal([]byte(jsonInput), &document)
	if err != nil {
		log.Printf("Error parsing JSON: %v", err)
		return
	}

	// Attempt to unstringify JSON for both objects and arrays
	unstrDocument, err := unstringifyJSON(document)
	if err != nil {
		log.Printf("Error unstringifying JSON: %v", err)
		return
	}

	// Process the document based on its type (object or array)
	switch docTyped := unstrDocument.(type) {
	case map[string]interface{}:
		// It's a JSON object
		processDocument(docTyped, col)
	case []interface{}:
		// It's a JSON array, iterate over elements if needed
		for _, item := range docTyped {
			if itemMap, ok := item.(map[string]interface{}); ok {
				processDocument(itemMap, col)
			}
		}
	default:
		log.Println("JSON is neither an object nor an array after unstringification")
	}
}

func processDocument(document map[string]interface{}, col *gocb.Collection) {
	// Get the current Unix timestamp
	currentTimestamp := time.Now().Unix()

	// Check if _seen_at_first is present in the document
	isInsert := false
	if _, exists := document["_seen_at_first"]; !exists {
		// Set _seen_at_first and _seen_at_last for newly created documents
		document["_seen_at_first"] = currentTimestamp
		document["_seen_at_last"] = currentTimestamp
		isInsert = true
	} else {
		// Update only _seen_at_last for existing documents
		document["_seen_at_last"] = currentTimestamp
	}

	documentID, ok := document["id"].(string)
	if !ok || documentID == "" {
		log.Println("Document ID ('id' field) is missing or not a string")
		return
	}

	_, err := col.Upsert(documentID, document, &gocb.UpsertOptions{})
	if err != nil {
		log.Printf("Failed to upsert document with ID %s: %v", documentID, err)
		return
	}

	if isInsert {
		log.Printf("Document with ID %s inserted successfully\n", documentID)
	} else {
		log.Printf("Document with ID %s upserted successfully\n", documentID)
	}
}

func unstringifyJSON(input interface{}) (interface{}, error) {
	switch v := input.(type) {
	case string:
		var temp interface{}
		if err := json.Unmarshal([]byte(v), &temp); err == nil {
			return unstringifyJSON(temp)
		}
		return v, nil
	case map[string]interface{}:
		for key, val := range v {
			unstrVal, err := unstringifyJSON(val)
			if err != nil {
				return nil, err
			}
			v[key] = unstrVal
		}
		return v, nil
	case []interface{}:
		for i, val := range v {
			unstrVal, err := unstringifyJSON(val)
			if err != nil {
				return nil, err
			}
			v[i] = unstrVal
		}
		return v, nil
	default:
		return input, nil
	}
}
