package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"github.com/couchbase/gocb/v2"
)

type Config struct {
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

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "", "Configuration file path")

	defaultConnStr := flag.String("conn", "localhost", "Couchbase connection string")
	defaultBucketName := flag.String("bucket", "all_nostr_events", "Bucket name")
	defaultUsername := flag.String("user", "admin", "Username")
	defaultPassword := flag.String("pass", "ore8airman7goods6feudal8mantle", "Password")
	defaultLogging := flag.Bool("v", false, "Verbose logging from 'gocb'")

	flag.Parse()

	config := Config{
		ConnectionString: *defaultConnStr,
		BucketName:       *defaultBucketName,
		Username:         *defaultUsername,
		Password:         *defaultPassword,
	}

	if configFile != "" {
		fileContent, err := ioutil.ReadFile(configFile)
		if err != nil {
			log.Fatalf("Error reading config file: %v", err)
		}
		if err := json.Unmarshal(fileContent, &config); err != nil {
			log.Fatalf("Error parsing config file: %v", err)
		}
	}

	if *defaultLogging {
		gocb.SetLogger(gocb.DefaultStdioLogger())
	}

	cluster, err := gocb.Connect(config.ConnectionString, gocb.ClusterOptions{
		Authenticator: gocb.PasswordAuthenticator{Username: config.Username, Password: config.Password},
	})
	if err != nil {
		log.Fatal(err)
	}

	bucket := cluster.Bucket(config.BucketName)
	err = bucket.WaitUntilReady(5*time.Second, nil)
	if err != nil {
		log.Fatal(err)
	}
	col := bucket.DefaultCollection()

	if filenames == "" {
		processFile(os.Stdin, col)
	} else {
		filesToProcess := strings.Split(filenames, " ")
		for _, filename := range filesToProcess {
			file, err := os.Open(filename)
			if err != nil {
				log.Fatalf("Cannot open file %s: %v", filename, err)
			}
			defer file.Close()

			processFile(file, col)
		}
	}
}

func processFile(file *os.File, col *gocb.Collection) {
	scanner := bufio.NewScanner(file)
	const maxBufferSize = 10 * 1024 * 1024      // Adjust the size as needed, e.g., 10MB
	buffer := make([]byte, 4096, maxBufferSize) // Initial size of 4KB, max of 10MB
	scanner.Buffer(buffer, maxBufferSize)

	for scanner.Scan() {
		processLine(scanner.Text(), col)
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
	documentID, ok := document["id"].(string)
	if !ok || documentID == "" {
		log.Println("Document ID ('id' field) is missing or not a string")
		return
	}

	_, err := col.Insert(documentID, document, &gocb.InsertOptions{})
	if err != nil {
		log.Printf("Failed to insert document with ID %s: %v", documentID, err)
		return
	}

	fmt.Printf("Document with ID %s inserted successfully\n", documentID)
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
