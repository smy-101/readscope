package main

import (
	"archive/zip"
	"fmt"
	billdaws "github.com/billdaws/epub"
	"github.com/ABFCode/Spine"
)

func main() {
	p := "/tmp/ziptest/pw-protected.epub"
	zr, err := zip.OpenReader(p)
	fmt.Println("stdlib zip.OpenReader err:", err)
	if err == nil {
		for _, f := range zr.File {
			rc, oerr := f.Open()
			if oerr != nil {
				fmt.Printf("  entry %s: open err: %v\n", f.Name, oerr)
			} else {
				rc.Close()
				fmt.Printf("  entry %s: open ok (encrypted flag not set or readable)\n", f.Name)
			}
		}
		zr.Close()
	}
	r, err := billdaws.Open(p)
	fmt.Println("billdaws.Open:", r != nil, err)
	if r != nil {
		defer r.Close()
	}
	b, err := spine.ParseFile(p)
	fmt.Println("spine.ParseFile: book != nil:", b != nil, "err:", err)
	if b != nil {
		defer b.Close()
		fmt.Println("  spine:", len(b.Spine), "warnings:", len(b.Warnings))
	}
}
