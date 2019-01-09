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

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/pulumi/tf2pulumi/convert"
	"github.com/pulumi/tf2pulumi/version"
)

func main() {
	var opts convert.Options
	flag.BoolVar(&opts.AllowMissingProviders, "allow-missing-plugins", false,
		"allows code generation to continue if resource provider plugins are missing")

	flag.Parse()

	args := flag.Args()
	if len(args) == 1 && args[0] == "version" {
		fmt.Println(version.Version)
		return
	}

	if err := convert.Convert(opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
		os.Exit(-1)
	}
}
