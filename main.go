// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/GoogleContainerTools/kpt-functions-sdk/go/fn"
	"gopkg.in/yaml.v3"
	"io"
	"os"
)

// metadata represents the standard kubernetes resource metadata.
type metadata struct {
	Annotations map[string]string `yaml:"annotations,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace,omitempty"`
}

// common represents properties that are shared by all kubernetes resources.
type common struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   metadata `yaml:"metadata"`
}

// secret represents a v1/Secret resource.
type secret struct {
	common     `yaml:",inline"`
	Type       string            `yaml:"type,omitempty"`
	StringData map[string]string `yaml:"stringData,omitempty"`
	Data       map[string]string `yaml:"data,omitempty"`
	Immutable  bool              `yaml:"immutable,omitempty"`
}

// ksopsGeneratorConfig represents a generator config for ksops.
type ksopsGeneratorConfig struct {
	common `yaml:",inline"`
	Files  []string `yaml:"files"`
}

// version is used to hold the version string. Is replaced at go build time
// with -ldflags.
var version = "development"

func main() {
	if err := mainCmd(); err != nil {
		fmt.Fprintln(os.Stderr, "ksops-dry-run:", err)
		os.Exit(1)
	}
}

func mainCmd() error {
	// Print version information and exit.
	if len(os.Args) >= 2 && os.Args[1] == "--version" {
		fmt.Fprintln(os.Stderr, "github.com/joshdk/ksops-dry-run version", version)

		return nil
	}

	// See https://github.com/viaduct-ai/kustomize-sops/blob/master/ksops.go
	// If one argument, assume KRM style
	if len(os.Args) == 1 {
		err := fn.AsMain(fn.ResourceListProcessorFunc(krm))
		if err != nil {
			return fmt.Errorf("unable to generate manifests: %w", err)
		}
		return nil
	}

	// If two argument, assume legacy style
	manifest, err := os.ReadFile(os.Args[1])
	if err != nil {
		return fmt.Errorf("unable to read in manifest: %s", os.Args[1])
	}

	return generate(manifest, os.Stdout)
}

// See https://github.com/viaduct-ai/kustomize-sops/blob/master/ksops.go
// https://pkg.go.dev/github.com/GoogleContainerTools/kpt-functions-sdk/go/fn#hdr-KRM_Function
func krm(rl *fn.ResourceList) (bool, error) {
	var items fn.KubeObjects
	for _, manifest := range rl.Items {
		var buf bytes.Buffer
		err := generate([]byte(manifest.String()), &buf)
		if err != nil {
			rl.LogResult(err)
			return false, err
		}

		// generate can return multiple manifests
		objs, err := fn.ParseKubeObjects(buf.Bytes())
		if err != nil {
			rl.LogResult(err)
			return false, err
		}

		items = append(items, objs...)
	}

	rl.Items = items

	return true, nil
}

func generate(raw []byte, out io.Writer) error {
	// Parse the ksops generator config.
	config, err := parseKsopsGenerator(raw)
	if err != nil {
		return err
	}

	// Set up a yaml stream encoder so that every (stubbed) secret resource can
	// be marshalled back to standard out with --- stream separators.
	encoder := yaml.NewEncoder(out)

	// Process each encrypted secret file in the config and output equivalent
	// secret resources with placeholder values.
	for _, filename := range config.Files {
		// Parse the (potentially multiple) secrets in this file, and generate
		// as many stubbed secrets.
		file, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		secrets, err := parseKsopsEncryptedSecrets(file)
		if err != nil {
			return err
		}

		// Encode each stubbed secret to the output stream.
		for _, secret := range secrets {
			if err := encoder.Encode(secret); err != nil {
				return err
			}
		}
	}

	return encoder.Close()
}

func parseKsopsGenerator(body []byte) (*ksopsGeneratorConfig, error) {
	var config ksopsGeneratorConfig
	if err := yaml.Unmarshal(body, &config); err != nil {
		return nil, err
	}

	// Sanity check the apiVersion and kind. This should never happen, as it
	// would be the result of a ksops generator misconfiguration.
	if config.APIVersion != "viaduct.ai/v1" {
		return nil, fmt.Errorf("expected ksops generator config apiVersion %q but got %q", "viaduct.ai/v1", config.APIVersion)
	} else if config.Kind != "ksops" {
		return nil, fmt.Errorf("expected ksops generator config kind %q but got %q", "ksops", config.APIVersion)
	}

	return &config, nil
}

func parseKsopsEncryptedSecrets(raw []byte) ([]secret, error) {
	// The decoder is used to read each yaml document from the stream one at a
	// time until no more are left.
	decoder := yaml.NewDecoder(bytes.NewReader(raw))

	var secrets []secret
	for {
		// Decode the next yaml document in the stream.
		var secret secret
		if err := decoder.Decode(&secret); err != nil {
			// No more yaml documents are left in the stream.
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, err
		}

		// Sanity check the apiVersion and kind.
		if secret.APIVersion != "v1" {
			return nil, fmt.Errorf("expected ksops encrypted secret apiVersion %q but got %q", "v1", secret.APIVersion)
		} else if secret.Kind != "Secret" {
			return nil, fmt.Errorf("expected ksops encrypted secret kind %q but got %q", "Secret", secret.Kind)
		}

		// Take the combined set of keys from both data and stringData, and
		// merge them into stringData with a placeholder value. The keys are
		// being merged into stringData (opposed to keeping both data and
		// stringData) for two reasons:
		// - To make it very obvious that the generated secrets have
		//   placeholder values to anyone who happens to e.g. read the stdout
		//   from kustomize build.
		// - To avoid needing to base64 encode said placeholder value. This
		//   would make things less obvious and is counter to the above point.
		// In the event that the original secret value was an empty string,
		// then preserve that empty string instead of using the placeholder
		// value. This is already viewable in the encrypted secret and assists
		// in understanding the overall configuration.
		if secret.StringData == nil {
			secret.StringData = make(map[string]string)
		}
		for key, value := range secret.StringData {
			switch value {
			case "": // Preserve the value if it is an empty string.
				secret.StringData[key] = ""
			default:
				secret.StringData[key] = "KSOPS_DRY_RUN_PLACEHOLDER"
			}
		}
		for key, value := range secret.Data {
			switch value {
			case "": // Preserve the value if it is an empty string.
				secret.StringData[key] = ""
			default:
				secret.StringData[key] = "KSOPS_DRY_RUN_PLACEHOLDER"
			}
		}
		secret.Data = nil

		// Add a custom label so that the user can use a label selector against the
		// generated resources to e.g. ignore them during a kubectl apply.
		if _, found := os.LookupEnv("KSOPS_DRY_RUN_ADD_LABEL"); found {
			if secret.Metadata.Labels == nil {
				secret.Metadata.Labels = make(map[string]string)
			}
			secret.Metadata.Labels["ksops-dry-run.joshdk.github.com"] = "true"
		}

		secrets = append(secrets, secret)
	}

	return secrets, nil
}
