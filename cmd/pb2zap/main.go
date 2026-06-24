// Command pb2zap rewrites protobuf message construction to zap-proto wire
// constructors across a Go tree.
//
//	pb2zap -map rules.txt [-w] ./dir...
//
// rules.txt holds one rule per line: "pbImportPath wireImportPath wirePkgName".
// Without -w it is a dry run (prints what it would change). It only rewrites
// construction (&pb.Msg{...} -> wire.NewMsg(wire.MsgInput{...})); the pb
// selectors it leaves are the field reads/writes a human still owns, reported
// as a count.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zap-proto/pb2zap"
)

func main() {
	write := flag.Bool("w", false, "write changes back to files (default: dry run)")
	mapFile := flag.String("map", "", "rules file: 'pbPath wirePath wireName' per line")
	flag.Parse()
	if *mapFile == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: pb2zap -map rules.txt [-w] <dir>...")
		os.Exit(2)
	}
	rules, err := loadRules(*mapFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pb2zap:", err)
		os.Exit(1)
	}

	var scanned, rewritten, pbLeft int
	for _, root := range flag.Args() {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.Contains(path, "/vendor/") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out, changed, left, rerr := pb2zap.Rewrite(path, src, rules)
			if rerr != nil {
				fmt.Fprintf(os.Stderr, "skip %s: %v\n", path, rerr)
				return nil
			}
			scanned++
			pbLeft += left
			if changed {
				rewritten++
				if *write {
					if err := os.WriteFile(path, out, 0o644); err != nil {
						return err
					}
					fmt.Println(path)
				} else {
					fmt.Println(path, "(dry-run)")
				}
			}
			return nil
		})
	}
	fmt.Printf("pb2zap: %d scanned, %d rewritten, %d pb selectors left for a human\n", scanned, rewritten, pbLeft)
}

func loadRules(file string) ([]pb2zap.Rule, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rules []pb2zap.Rule
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.Fields(line)
		if len(p) != 3 {
			return nil, fmt.Errorf("bad rule %q (want: pbPath wirePath wireName)", line)
		}
		rules = append(rules, pb2zap.Rule{PB: p[0], Wire: p[1], Name: p[2]})
	}
	return rules, sc.Err()
}
