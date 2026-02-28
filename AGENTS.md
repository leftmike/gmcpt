# gmcpt

## Build & Test Commands
All Go commands must be run with `go -C /home/mike/gmcpt`
```bash
go -C /home/mike/gmcpt build ./...
go -C /home/mike/gmcpt test -v ./...
go -C /home/mike/gmcpt test -v ./proxy/
```

## Critical
Never run `go` commands without `-C /home/mike/gmcpt`.

