package module

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/pulumi/tf2pulumi/internal/config"
)

func init() {
	if os.Getenv("TF_LOG") == "" {
		log.SetOutput(ioutil.Discard)
	}
}

const fixtureDir = "./testdata"

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", "tf")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("err: %s", err)
	}

	return dir
}

func testConfig(t *testing.T, n string) *config.Config {
	t.Helper()
	c, err := config.LoadDir(filepath.Join(fixtureDir, n))
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	return c
}

func testStorage(t *testing.T) *Storage {
	t.Helper()
	return NewStorage(tempDir(t))
}
