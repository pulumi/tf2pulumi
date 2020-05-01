package module

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	getter "github.com/hashicorp/go-getter"
	"github.com/mitchellh/cli"
)

const manifestName = "modules.json"

// moduleManifest is the serialization structure used to record the stored
// module's metadata.
type moduleManifest struct {
	Modules []moduleRecord
}

// moduleRecords represents the stored module's metadata.
// This is compared for equality using '==', so all fields needs to remain
// comparable.
type moduleRecord struct {
	// Source is the module source string from the config, minus any
	// subdirectory.
	Source string

	// Key is the locally unique identifier for this module.
	Key string

	// Version is the exact version string for the stored module.
	Version string

	// Dir is the directory name returned by the FileStorage. This is what
	// allows us to correlate a particular module version with the location on
	// disk.
	Dir string

	// Root is the root directory containing the module. If the module is
	// unpacked from an archive, and not located in the root directory, this is
	// used to direct the loader to the correct subdirectory. This is
	// independent from any subdirectory in the original source string, which
	// may traverse further into the module tree.
	Root string

	// url is the location of the module source
	url string

	// Registry is true if this module is sourced from a registry
	registry bool
}

// Storage implements methods to manage the storage of modules.
// This is used by Tree.Load to query registries, authenticate requests, and
// store modules locally.
type Storage struct {
	// StorageDir is the full path to the directory where all modules will be
	// stored.
	StorageDir string

	// Ui is an optional cli.Ui for user output
	Ui cli.Ui

	// Mode is the GetMode that will be used for various operations.
	Mode GetMode
}

// NewStorage returns a new initialized Storage object.
func NewStorage(dir string) *Storage {
	return &Storage{
		StorageDir: dir,
	}
}

// loadManifest returns the moduleManifest file from the parent directory.
func (s Storage) loadManifest() (moduleManifest, error) {
	manifest := moduleManifest{}

	manifestPath := filepath.Join(s.StorageDir, manifestName)
	data, err := ioutil.ReadFile(manifestPath)
	if err != nil && !os.IsNotExist(err) {
		return manifest, err
	}

	if len(data) == 0 {
		return manifest, nil
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

// Store the location of the module, along with the version used and the module
// root directory. The storage method loads the entire file and rewrites it
// each time. This is only done a few times during init, so efficiency is
// not a concern.
func (s Storage) recordModule(rec moduleRecord) error {
	manifest, err := s.loadManifest()
	if err != nil {
		// if there was a problem with the file, we will attempt to write a new
		// one. Any non-data related error should surface there.
		log.Printf("[WARN] error reading module manifest: %s", err)
	}

	// do nothing if we already have the exact module
	for i, stored := range manifest.Modules {
		if rec == stored {
			return nil
		}

		// they are not equal, but if the storage path is the same we need to
		// remove this rec to be replaced.
		if rec.Dir == stored.Dir {
			manifest.Modules[i] = manifest.Modules[len(manifest.Modules)-1]
			manifest.Modules = manifest.Modules[:len(manifest.Modules)-1]
			break
		}
	}

	manifest.Modules = append(manifest.Modules, rec)

	js, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}

	manifestPath := filepath.Join(s.StorageDir, manifestName)
	return ioutil.WriteFile(manifestPath, js, 0644)
}

// load the manifest from dir, and return all module versions matching the
// provided source. Records with no version info will be skipped, as they need
// to be uniquely identified by other means.
func (s Storage) moduleVersions(source string) ([]moduleRecord, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return manifest.Modules, err
	}

	var matching []moduleRecord

	for _, m := range manifest.Modules {
		if m.Source == source && m.Version != "" {
			log.Printf("[DEBUG] found local version %q for module %s", m.Version, m.Source)
			matching = append(matching, m)
		}
	}

	return matching, nil
}

func (s Storage) moduleDir(key string) (string, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return "", err
	}

	for _, m := range manifest.Modules {
		if m.Key == key {
			return m.Dir, nil
		}
	}

	return "", nil
}

// return only the root directory of the module stored in dir.
func (s Storage) getModuleRoot(dir string) (string, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return "", err
	}

	for _, mod := range manifest.Modules {
		if mod.Dir == dir {
			return mod.Root, nil
		}
	}
	return "", nil
}

// record only the Root directory for the module stored at dir.
func (s Storage) recordModuleRoot(dir, root string) error {
	rec := moduleRecord{
		Dir:  dir,
		Root: root,
	}

	return s.recordModule(rec)
}

func (s Storage) output(msg string) {
	if s.Ui == nil || s.Mode == GetModeNone {
		return
	}
	s.Ui.Output(msg)
}

func (s Storage) getStorage(key string, src string) (string, bool, error) {
	storage := &getter.FolderStorage{
		StorageDir: s.StorageDir,
	}

	log.Printf("[DEBUG] fetching module from %s", src)

	// Get the module with the level specified if we were told to.
	if s.Mode > GetModeNone {
		log.Printf("[DEBUG] fetching %q with key %q", src, key)
		if err := storage.Get(key, src, s.Mode == GetModeUpdate); err != nil {
			return "", false, err
		}
	}

	// Get the directory where the module is.
	dir, found, err := storage.Dir(key)
	log.Printf("[DEBUG] found %q in %q: %t", src, dir, found)
	return dir, found, err
}

// find a stored module that's not from a registry
func (s Storage) findModule(key string) (string, error) {
	if s.Mode == GetModeUpdate {
		return "", nil
	}

	return s.moduleDir(key)
}

// GetModule fetches a module source into the specified directory. This is used
// as a convenience function by the CLI to initialize a configuration.
func (s Storage) GetModule(dst, src string) error {
	// reset this in case the caller was going to re-use it
	mode := s.Mode
	s.Mode = GetModeUpdate
	defer func() {
		s.Mode = mode
	}()

	rec, err := s.findRegistryModule(src, anyVersion)
	if err != nil {
		return err
	}

	pwd, err := os.Getwd()
	if err != nil {
		return err
	}

	source := rec.url
	if source == "" {
		source, err = getter.Detect(src, pwd, getter.Detectors)
		if err != nil {
			return fmt.Errorf("module %s: %s", src, err)
		}
	}

	if source == "" {
		return fmt.Errorf("module %q not found", src)
	}

	return GetCopy(dst, source)
}

// find a registry module
func (s Storage) findRegistryModule(mSource, constraint string) (moduleRecord, error) {
	rec := moduleRecord{
		Source: mSource,
	}
	return rec, nil
}
