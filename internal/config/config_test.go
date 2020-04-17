package config

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hashicorp/hil/ast"
)

// This is the directory where our test fixtures are.
const fixtureDir = "./testdata"

func TestMain(m *testing.M) {
	flag.Parse()

	// silence all logs
	log.SetOutput(ioutil.Discard)

	os.Exit(m.Run())
}

func TestConfigCopy(t *testing.T) {
	c := testConfig(t, "copy-basic")
	rOrig := c.Resources[0]
	rCopy := rOrig.Copy()

	if rCopy.Name != rOrig.Name {
		t.Fatalf("Expected names to equal: %q <=> %q", rCopy.Name, rOrig.Name)
	}

	if rCopy.Type != rOrig.Type {
		t.Fatalf("Expected types to equal: %q <=> %q", rCopy.Type, rOrig.Type)
	}

	origCount := rOrig.RawCount.Config()["count"]
	rCopy.RawCount.Config()["count"] = "5"
	if rOrig.RawCount.Config()["count"] != origCount {
		t.Fatalf("Expected RawCount to be copied, but it behaves like a ref!")
	}

	rCopy.RawConfig.Config()["newfield"] = "hello"
	if rOrig.RawConfig.Config()["newfield"] == "hello" {
		t.Fatalf("Expected RawConfig to be copied, but it behaves like a ref!")
	}

	rCopy.Provisioners = append(rCopy.Provisioners, &Provisioner{})
	if len(rOrig.Provisioners) == len(rCopy.Provisioners) {
		t.Fatalf("Expected Provisioners to be copied, but it behaves like a ref!")
	}

	if rCopy.Provider != rOrig.Provider {
		t.Fatalf("Expected providers to equal: %q <=> %q",
			rCopy.Provider, rOrig.Provider)
	}

	rCopy.DependsOn[0] = "gotchya"
	if rOrig.DependsOn[0] == rCopy.DependsOn[0] {
		t.Fatalf("Expected DependsOn to be copied, but it behaves like a ref!")
	}

	rCopy.Lifecycle.IgnoreChanges[0] = "gotchya"
	if rOrig.Lifecycle.IgnoreChanges[0] == rCopy.Lifecycle.IgnoreChanges[0] {
		t.Fatalf("Expected Lifecycle to be copied, but it behaves like a ref!")
	}

}

func TestConfigCount(t *testing.T) {
	c := testConfig(t, "count-int")
	actual, err := c.Resources[0].Count()
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if actual != 5 {
		t.Fatalf("bad: %#v", actual)
	}
}

func TestConfigCount_string(t *testing.T) {
	c := testConfig(t, "count-string")
	actual, err := c.Resources[0].Count()
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if actual != 5 {
		t.Fatalf("bad: %#v", actual)
	}
}

// Terraform GH-11800
func TestConfigCount_list(t *testing.T) {
	c := testConfig(t, "count-list")

	// The key is to interpolate so it doesn't fail parsing
	c.Resources[0].RawCount.Interpolate(map[string]ast.Variable{
		"var.list": ast.Variable{
			Value: []ast.Variable{},
			Type:  ast.TypeList,
		},
	})

	_, err := c.Resources[0].Count()
	if err == nil {
		t.Fatal("should error")
	}
}

func TestConfigCount_var(t *testing.T) {
	c := testConfig(t, "count-var")
	_, err := c.Resources[0].Count()
	if err == nil {
		t.Fatalf("should error")
	}
}

func TestConfig_emptyCollections(t *testing.T) {
	c := testConfig(t, "empty-collections")
	if len(c.Variables) != 3 {
		t.Fatalf("bad: expected 3 variables, got %d", len(c.Variables))
	}
	for _, variable := range c.Variables {
		switch variable.Name {
		case "empty_string":
			if variable.Default != "" {
				t.Fatalf("bad: wrong default %q for variable empty_string", variable.Default)
			}
		case "empty_map":
			if !reflect.DeepEqual(variable.Default, map[string]interface{}{}) {
				t.Fatalf("bad: wrong default %#v for variable empty_map", variable.Default)
			}
		case "empty_list":
			if !reflect.DeepEqual(variable.Default, []interface{}{}) {
				t.Fatalf("bad: wrong default %#v for variable empty_list", variable.Default)
			}
		default:
			t.Fatalf("Unexpected variable: %s", variable.Name)
		}
	}
}

func TestNameRegexp(t *testing.T) {
	cases := []struct {
		Input string
		Match bool
	}{
		{"hello", true},
		{"foo-bar", true},
		{"foo_bar", true},
		{"_hello", true},
		{"foo bar", false},
		{"foo.bar", false},
	}

	for _, tc := range cases {
		if NameRegexp.Match([]byte(tc.Input)) != tc.Match {
			t.Fatalf("Input: %s\n\nExpected: %#v", tc.Input, tc.Match)
		}
	}
}

func TestProviderConfigName(t *testing.T) {
	pcs := []*ProviderConfig{
		&ProviderConfig{Name: "aw"},
		&ProviderConfig{Name: "aws"},
		&ProviderConfig{Name: "a"},
		&ProviderConfig{Name: "gce_"},
	}

	n := ProviderConfigName("aws_instance", pcs)
	if n != "aws" {
		t.Fatalf("bad: %s", n)
	}
}

func testConfig(t *testing.T, name string) *Config {
	c, err := LoadFile(filepath.Join(fixtureDir, name, "main.tf"))
	if err != nil {
		t.Fatalf("file: %s\n\nerr: %s", name, err)
	}

	return c
}

func TestConfigDataCount(t *testing.T) {
	c := testConfig(t, "data-count")
	actual, err := c.Resources[0].Count()
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if actual != 5 {
		t.Fatalf("bad: %#v", actual)
	}

	// we need to make sure "count" has been removed from the RawConfig, since
	// it's not a real key and won't validate.
	if _, ok := c.Resources[0].RawConfig.Raw["count"]; ok {
		t.Fatal("count key still exists in RawConfig")
	}
}

func TestConfigProviderVersion(t *testing.T) {
	c := testConfig(t, "provider-version")

	if len(c.ProviderConfigs) != 1 {
		t.Fatal("expected 1 provider")
	}

	p := c.ProviderConfigs[0]
	if p.Name != "aws" {
		t.Fatalf("expected provider name 'aws', got %q", p.Name)
	}

	if p.Version != "0.0.1" {
		t.Fatalf("expected providers version '0.0.1', got %q", p.Version)
	}

	if _, ok := p.RawConfig.Raw["version"]; ok {
		t.Fatal("'version' should not exist in raw config")
	}
}

func TestResourceProviderFullName(t *testing.T) {
	type testCase struct {
		ResourceName string
		Alias        string
		Expected     string
	}

	tests := []testCase{
		{
			// If no alias is provided, the first underscore-separated segment
			// is assumed to be the provider name.
			ResourceName: "aws_thing",
			Alias:        "",
			Expected:     "aws",
		},
		{
			// If we have more than one underscore then it's the first one that we'll use.
			ResourceName: "aws_thingy_thing",
			Alias:        "",
			Expected:     "aws",
		},
		{
			// A provider can export a resource whose name is just the bare provider name,
			// e.g. because the provider only has one resource and so any additional
			// parts would be redundant.
			ResourceName: "external",
			Alias:        "",
			Expected:     "external",
		},
		{
			// Alias always overrides the default extraction of the name
			ResourceName: "aws_thing",
			Alias:        "tls.baz",
			Expected:     "tls.baz",
		},
	}

	for _, test := range tests {
		got := ResourceProviderFullName(test.ResourceName, test.Alias)
		if got != test.Expected {
			t.Errorf(
				"(%q, %q) produced %q; want %q",
				test.ResourceName, test.Alias,
				got,
				test.Expected,
			)
		}
	}
}

func TestConfigModuleProviders(t *testing.T) {
	c := testConfig(t, "module-providers")

	if len(c.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(c.Modules))
	}

	expected := map[string]string{
		"aws": "aws.foo",
	}

	got := c.Modules[0].Providers

	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("exptected providers %#v, got providers %#v", expected, got)
	}
}
