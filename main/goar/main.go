package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/zncoder/goar"
)

func main() {
	archiveOpt := flag.Bool("a", false, "pack files to go binary")
	inspectOpt := flag.Bool("t", false, "inspect goar file")
	trimOpt := flag.Bool("r", false, "trim goar file to restore the original binary")
	extractOpt := flag.Bool("e", false, "extract files")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage:
  goar -t <go_binary>
  goar -a <go_binary> <file>...
  goar -r <go_binary>
  goar -e <go_binary> <file>...

`)
		flag.PrintDefaults()
		os.Exit(2)
	}
	flag.Parse()

	switch {
	case *archiveOpt:
		if flag.NArg() < 2 {
			flag.Usage()
		}
		archive(flag.Arg(0), flag.Args()[1:])

	case *inspectOpt:
		if flag.NArg() != 1 {
			flag.Usage()
		}
		inspect(flag.Arg(0))

	case *trimOpt:
		if flag.NArg() != 1 {
			flag.Usage()
		}
		trim(flag.Arg(0))

	case *extractOpt:
		if flag.NArg() < 1 {
			flag.Usage()
		}
		extract(flag.Arg(0), flag.Args()[1:])

	default:
		flag.Usage()
	}
}

func inspect(fn string) {
	fs, err := goar.NewFileSystem(fn)
	if err != nil {
		log.Fatalf("inspect file:%s err:%v", fn, err)
	}
	defer fs.Close()

	fmt.Printf("Size of binary: %d\n", fs.BinarySize)
	for _, fi := range fs.List() {
		fmt.Printf("  %s => %d\n", fi.Name, fi.Size)
	}
}

func archive(binfn string, fns []string) {
	ar, err := goar.NewArchiver(binfn)
	if err != nil {
		log.Fatalf("new archiver err:%v", err)
	}
	defer ar.Close()

	for _, fn := range fns {
		if err = ar.Add(fn); err != nil {
			log.Fatalf("add file:%s err:%v", fn, err)
		}
		log.Printf("file:%s added", fn)
	}
}

func trim(fn string) {
	fs, err := goar.NewFileSystem(fn)
	if err != nil {
		log.Fatalf("open goar file:%s err:%v", fn, err)
	}
	sz := fs.BinarySize
	fs.Close()

	if err = os.Truncate(fn, sz); err != nil {
		log.Fatalf("truncate file:%s to size:%d err:%v", fn, sz, err)
	}
	log.Printf("trim to %d", sz)
}

func extract(binfn string, fns []string) {
	fs, err := goar.NewFileSystem(binfn)
	if err != nil {
		log.Fatalf("inspect file:%s err:%v", binfn, err)
	}
	defer fs.Close()

	if len(fns) == 0 {
		for _, fi := range fs.List() {
			fns = append(fns, fi.Name)
		}
	}

	for _, fn := range fns {
		f, err := fs.Open(fn)
		if err != nil {
			log.Printf("fail to extract file:%s err:%v", fn, err)
			continue
		}
		dn := filepath.Dir(fn)
		if err := os.MkdirAll(dn, 0755); err != nil {
			log.Fatalf("cannot make dir:%s err:%v", dn, err)
		}
		out, err := os.Create(fn)
		if err != nil {
			log.Fatalf("cannot create file:%s err:%v", fn, err)
		}
		_, err = io.Copy(out, f)
		if err != nil {
			log.Fatalf("fail to extract file:%s err:%v", fn, err)
		}
		f.Close()
		out.Close()
	}
}
