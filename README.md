# xn2

Pull, synchronize and package data on interval.

### Development

Build and test **xn2** with `docker-compose up`. `docker-compose` starts a fake api server and configures **xn2** poll metrics from it. **xn2** and all other services are monitored with **prometheus** running at http://localhost:9090.

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