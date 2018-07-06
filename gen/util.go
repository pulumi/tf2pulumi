package gen

import (
	"reflect"
	"sort"

	"github.com/pulumi/pulumi/pkg/util/contract"
)

// SortedKeys returns a sorted list of keys for the given map. The map's key type must be of kind string.
func SortedKeys(m interface{}) []string {
	mv := reflect.ValueOf(m)

	contract.Require(mv.Type().Kind() == reflect.Map, "m")
	contract.Require(mv.Type().Key().Kind() == reflect.String, "m")

	keys := make([]string, mv.Len())
	for i, k := range mv.MapKeys() {
		keys[i] = k.String()
	}
	sort.Strings(keys)

	return keys
}
