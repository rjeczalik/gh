package webhook

import (
	"bytes"
	"crypto/sha1"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hash(r io.Reader) ([]byte, error) {
	h := sha1.New()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func testFiles(t *testing.T, files map[string]string) {
	for orig, dump := range files {
		forig, err := os.Open(orig)
		if err != nil {
			t.Errorf("os.Open(%q)=%v", orig, err)
			continue
		}
		fdump, err := os.Open(dump)
		if err != nil {
			t.Errorf("os.Open(%q)=%v", orig, nonil(err, forig.Close()))
			continue
		}
		horig, err := hash(forig)
		if err != nil {
			t.Errorf("hashing %s failed: %v", orig, nonil(err, forig.Close(), fdump.Close()))
			continue
		}
		hdump, err := hash(fdump)
		if err != nil {
			t.Errorf("hashing %s failed: %v", dump, nonil(err, forig.Close(), fdump.Close()))
			continue
		}
		if !bytes.Equal(horig, hdump) {
			t.Errorf("files %q and %q are not equal", orig, dump)
		}
	}
}

func TestDump(t *testing.T) {
	tmp, err := ioutil.TempDir("", "testdump")
	if err != nil {
		t.Fatalf("ioutil.TempDir()=%v", err)
	}
	defer os.RemoveAll(tmp)
	testHandler(t, Dump(tmp, New(secret, BlanketHandler{})))
	fis, err := ioutil.ReadDir(tmp)
	if err != nil {
		t.Fatalf("ioutil.ReadDir(%q)=%v", tmp, err)
	}
	if len(fis) != len(payloads) {
		t.Fatalf("want number of dumped files be %d; got %d", len(payloads), len(fis))
	}
	files := make(map[string]string)
	for _, fi := range fis {
		n := strings.IndexRune(fi.Name(), '-')
		if n == -1 {
			t.Fatalf("unexpected file name: %s", fi.Name())
		}
		event := fi.Name()[:n]
		if _, ok := payloads[event]; !ok {
			t.Fatalf("Dump written a file for a non-existing event: %s", event)
		}
		orig := filepath.Join("testdata", event+".json")
		if _, ok := files[orig]; ok {
			t.Fatalf("duplicated files for the %s event", event)
		}
		files[orig] = filepath.Join(tmp, fi.Name())
	}
	testFiles(t, files)
}
