# xn2

Pull, synchronize and package data on interval. Xn2 is a data scraper / muxer, combining data from multiple URLs defined by a set.

See the example configuration used for testing with `docker-compose up`: [simple.yml](./config/xn2/simple.yml)

Build and test **xn2** with `docker-compose up`. `docker-compose` starts a fake api server and configures **xn2** poll metrics from it. **xn2** and all other services are monitored with **prometheus**.

The diagram below shows the flow of data from a **set**.

### Demo

<p align="center">
  <img width="680" height="879" src="./assets/xn2_demo_diagram.png" alt="kubefwd - Kubernetes Port Forward Diagram">
</p>


```bash
docker-compose up --build
```

Run source with local config and no destination:

```bash
go run ./cmd/xn2.go --port 8282 --config ./config/xn2/local.yml
```

While running check **prometheus** metrics at https://localhost:8282/metrics


### Release Management

Test release:
```bash
goreleaser --skip-publish --rm-dist --skip-validate
```

Release
```bash
GITHUB_TOKEN=$GITHUB_TOKEN goreleaser --rm-dist
```