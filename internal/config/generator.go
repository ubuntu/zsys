// +build tools

package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/shurcooL/httpfs/filter"
	"github.com/shurcooL/vfsgen"
)

func main() {
	var configDir http.FileSystem = http.Dir(".")
	fs := filter.Skip(configDir, func(path string, fi os.FileInfo) bool {
		return filepath.Ext(path) == ".go"
	})
	err := vfsgen.Generate(fs, vfsgen.Options{
		PackageName:  "config",
		VariableName: "internalAssets",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
