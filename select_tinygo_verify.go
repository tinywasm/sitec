package sitec

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tinywasm/fmt"
)

// VerifyTinyGoCompatible checks if the source tree is compatible with TinyGo compilation.
// It reports why the source tree would not compile with TinyGo, or nil if compatible.
func VerifyTinyGoCompatible(dir string) error {
	problematicImports := []string{"fmt", "strings", "strconv"}
	var foundIssues []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if filepath.Ext(path) != ".go" || filepath.Base(path) == "verify_tinygo.go" {
			return nil
		}

		// Skip test files since they're not part of the compiled library
		fileName := filepath.Base(path)
		if len(fileName) > 8 && fileName[len(fileName)-8:] == "_test.go" {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Read file content
		buffer := make([]byte, 1024)
		n, _ := file.Read(buffer)
		content := string(buffer[:n])
		for _, imp := range problematicImports {
			importStr := "\"" + imp + "\""
			if strings.Contains(content, importStr) {
				foundIssues = append(foundIssues, "problematic import "+imp+" in "+path)
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	if len(foundIssues) > 0 {
		return fmt.Err(strings.Join(foundIssues, "; "))
	}
	return nil
}
