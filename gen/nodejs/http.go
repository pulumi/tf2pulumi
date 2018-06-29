package nodejs

import (
	"bytes"
	"fmt"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/util/contract"

	"github.com/pgavlin/firewalker/il"
)

func (g *Generator) computeHTTPInputs(r *il.ResourceNode, indent, count string) (string, error) {
	urlProperty, ok := r.Properties.Elements["url"]
	if !ok {
		return "", errors.Errorf("missing required property \"url\" in resource %s", r.Config.Name)
	}
	url, err := g.computeProperty(urlProperty, indent, count)
	if err != nil {
		return "", err
	}

	requestHeadersProperty, hasRequestHeaders := r.Properties.Elements["request_headers"]
	if !hasRequestHeaders {
		return url, nil
	}

	requestHeaders, err := g.computeProperty(requestHeadersProperty, indent, count)
	if err != nil {
		return "", err
	}

	buf := &bytes.Buffer{}
	buf.WriteString("{\n")
	fmt.Fprintf(buf, "%s    url: %s,\n", indent, url)
	fmt.Fprintf(buf, "%s    headers: %s,\n", indent, requestHeaders)
	fmt.Fprintf(buf, "%s}", indent)
	return buf.String(), nil
}

func (g *Generator) generateHTTP(r *il.ResourceNode) error {
	contract.Require(r.Provider.Config.Name == "http", "r")

	name := resName(r.Config.Type, r.Config.Name)

	if r.Count == nil {
		inputs, err := g.computeHTTPInputs(r, "", "")
		if err != nil {
			return err
		}

		fmt.Printf("const %s = pulumi.output(rpn(%s).promise());\n", name, inputs);
	} else {
		count, err := g.computeProperty(r.Count, "", "")
		if err != nil {
			return err
		}
		inputs, err := g.computeHTTPInputs(r, "    ", "i")
		if err != nil {
			return err
		}

		fmt.Printf("const %s: pulumi.Output<string>[] = [];\n", name)
		fmt.Printf("for (let i = 0; i < %s; i++) {\n", count)
		fmt.Printf("    %s.push(pulumi.output(rpn(%s).promise()));\n", name, inputs)
		fmt.Printf("}\n")
	}

	return nil
}

