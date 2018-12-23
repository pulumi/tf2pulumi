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

package nodejs

import (
	"bytes"
	"fmt"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pulumi/tf2pulumi/il"
)

// computeHTTPInputs computes the arguments for a call to request-promise-native's single function from the bound input
// properties of the given http resource.
func (g *generator) computeHTTPInputs(r *il.ResourceNode, indent bool, count string) (string, error) {
	urlProperty, ok := r.Properties.Elements["url"]
	if !ok {
		return "", errors.Errorf("missing required property \"url\" in resource %s", r.Config.Name)
	}
	url, _, err := g.computeProperty(urlProperty, indent, count)
	if err != nil {
		return "", err
	}

	requestHeadersProperty, hasRequestHeaders := r.Properties.Elements["request_headers"]
	if !hasRequestHeaders {
		return url, nil
	}

	requestHeaders, _, err := g.computeProperty(requestHeadersProperty, true, count)
	if err != nil {
		return "", err
	}

	buf := &bytes.Buffer{}
	buf.WriteString("{\n")
	fmt.Fprintf(buf, "%s    url: %s,\n", g.indent, url)
	fmt.Fprintf(buf, "%s    headers: %s,\n", g.indent, requestHeaders)
	fmt.Fprintf(buf, "%s}", g.indent)
	return buf.String(), nil
}

// generateHTTP generates the given http resource as a call to request-promise-native's single exported function.
func (g *generator) generateHTTP(r *il.ResourceNode) error {
	contract.Require(r.Provider.Config.Name == "http", "r")

	name := resName(r.Config.Type, r.Config.Name)

	if r.Count == nil {
		inputs, err := g.computeHTTPInputs(r, false, "")
		if err != nil {
			return err
		}

		g.printf("const %s = pulumi.output(rpn(%s).promise());\n", name, inputs)
	} else {
		count, _, err := g.computeProperty(r.Count, false, "")
		if err != nil {
			return err
		}
		inputs, err := g.computeHTTPInputs(r, true, "i")
		if err != nil {
			return err
		}

		g.printf("const %s: pulumi.Output<string>[] = [];\n", name)
		g.printf("for (let i = 0; i < %s; i++) {\n", count)
		g.printf("    %s.push(pulumi.output(rpn(%s).promise()));\n", name, inputs)
		g.printf("}\n")
	}

	return nil
}
