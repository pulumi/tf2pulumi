// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gen

import (
	"reflect"
	"sort"

	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"
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
