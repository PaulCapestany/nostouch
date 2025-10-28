# nostouch

## Project Purpose
`nostouch` is a service meant to run on an OpenShift cluster that focuses on ingesting all NOSTR event JSON data from a NOSTR relay service called strfry and upserting it into Couchbase. NOSTR events may have stringified data, and because we want to be able to easily parse all data for indexing on Couchbase, any stringified data should be converted to valid JSON.

## Goals

The primary goals of the `nostouch` project are:

1. **Ingest Data**
2. **Unstringify Data**
3. **Upsert Data Into Couchbase**

## TODO

## Getting Started

### Prerequisites
- [Go 1.24.1](https://golang.org/dl/)
- [Couchbase Server 7.6.2](https://www.couchbase.com/downloads)
- [gocb v2.8.1](https://github.com/couchbase/gocb)
- [Nostr protocol specification](https://github.com/nostr-protocol/nips)
- Ensure a *all-nostr-events* Couchbase bucket exists
- Ensure the following Couchbase index exists in the *all-nostr-events* bucket:
```sql
CREATE INDEX `kind_and_event_lookup` ON `default`:`all-nostr-events`.`_default`.`_default`(`kind`,(distinct (array (`t`[1]) for `t` in `tags` when ((`t`[0]) = "e") end))) PARTITION BY HASH(META().id) WITH {"num_replica": 1}
```

### Installation
1. Clone the repository:
    ```shell
    git clone https://github.com/paulcapestany/nostouch.git
    cd nostouch
    ```
2. Install dependencies:
    ```shell
    go mod tidy
    ```
3. Build/test:
   ```shell
   go mod tidy && go install ./... && go test -v
   ``` 

### Usage
1. Start the service:
    ```shell
    printf '{"since":1716200000}' | ./nak req ws://127.0.0.1:7777 | ./nostouch
    ```
2. 

### Future Directions

For now, `nostouch` is meant as the quickest way to get to a demo for a proof of concept.

## Contributing
1. Fork the repository.
2. Create a new branch for your feature/bugfix.
3. Submit a pull request with a detailed description of your changes.

## Contact

For any questions or suggestions, please contact [Paul](http://github.com/paulcapestany).


---
