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

package il

// FilterProperties removes any properties at the root of the given resource for which the given filter function
// returns false.
func FilterProperties(r *ResourceNode, filter func(key string, property BoundNode) bool) {
	for key, prop := range r.Properties.Elements {
		if !filter(key, prop) {
			delete(r.Properties.Elements, key)
		}
	}
}
