# pb2zap

Migrate a Go codebase off Protocol Buffers and onto [ZAP](https://github.com/zap-proto/go) — zero-copy wire, no `Marshal`/`Unmarshal`, no gRPC.

`pb2zap` is a `go/ast` codemod. It is **syntactic** — it does not type-check —
so it runs on a tree mid-migration that does not yet compile.

```
go install github.com/zap-proto/pb2zap/cmd/pb2zap@latest
pb2zap -map rules.txt -w ./...
```

`rules.txt` is one rule per line — `pbImportPath wireImportPath wirePkgName`:

```
github.com/acme/app/pb/mount_pb  github.com/acme/app/wire/mount  mountwire
github.com/acme/app/pb/filer_pb  github.com/acme/app/wire/filer  filerwire
```

## What it rewrites

The one transform that is unambiguous from syntax alone — **construction**:

```go
&mount_pb.ConfigureRequest{CollectionCapacity: n}
// becomes
mountwire.NewConfigureRequest(mountwire.ConfigureRequestInput{CollectionCapacity: n})
```

It adds the wire import, drops the pb import once nothing else references it,
and **reports** (never guesses) the pb selectors it leaves — the field reads
that need accessors and the writes that immutable ZAP cannot express.

## What it deliberately does not do

pb→ZAP is not a 1:1 transform; it is a **data-flow split**. A `*pb.Msg` is one
type used to both build (set fields) and read (get fields). ZAP splits that
into three — `Input` (build) → `[]byte` (transport) → `View` (read). A value
that is constructed, threaded through several functions, mutated, then read has
no single ZAP type; it has to be restructured into build-once / read-many.
`pb2zap` does the safe mechanical piece and points a human at the rest, rather
than emit code that compiles by accident.

## Library

```go
out, changed, pbLeft, err := pb2zap.Rewrite("file.go", src, rules)
```

Pure function, `golang.org/x/tools` only. See [the migration guide](https://github.com/zap-proto/go/blob/main/docs/migrate-from-protobuf.md).
