# xn2
WIP: pull, synchronize and package data on interval. 


### Development

Start a test API endpoint.

```bash
docker run --rm -p 8080:8080 txn2/fxapi:latest
```

Start receiving with rxtx:

```bash
docker-compose up
```

Run source
```bash
go run ./cmd/xn2.go --debug true --port 8082 --config ./examples/simple.yml
```