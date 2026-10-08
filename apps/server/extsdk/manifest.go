package extsdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// APIVersion is the extension API this host implements. An extension declares
// the version it was written for in engines.sdk; the host refuses any other.
const APIVersion = 1

// IDPattern is what an extension id must match.
const IDPattern = `^[a-z][a-z0-9_]{0,31}$`

var (
	idRe     = regexp.MustCompile(IDPattern)
	semverRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
)

// manifestFile is the shape of pp-app.json.
type manifestFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	License     string `json:"license"`
	Homepage    string `json:"homepage"`
	Engines     struct {
		SDK *int `json:"sdk"`
	} `json:"engines"`
	NodeKinds []struct {
		Kind string `json:"kind"`
	} `json:"nodeKinds"`
	Server struct {
		Package string `json:"package"`
	} `json:"server"`
	Web struct {
		Package string `json:"package"`
	} `json:"web"`
}

// ParseManifest reads an extension's pp-app.json strictly: unknown fields are
// an error, and the id, version, SDK version and node kinds are validated. The
// result carries no Migrations or hooks; the extension attaches those.
func ParseManifest(data []byte) (Manifest, error) {
	var f manifestFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Manifest{}, fmt.Errorf("pp-app.json: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("pp-app.json: trailing data after the manifest")
	}
	if !idRe.MatchString(f.ID) {
		return Manifest{}, fmt.Errorf("pp-app.json: id %q must match %s", f.ID, IDPattern)
	}
	if strings.TrimSpace(f.Name) == "" {
		return Manifest{}, errors.New("pp-app.json: name is required")
	}
	if !semverRe.MatchString(f.Version) {
		return Manifest{}, fmt.Errorf("pp-app.json: version %q is not a semantic version", f.Version)
	}
	if f.Engines.SDK == nil || *f.Engines.SDK < 1 {
		return Manifest{}, errors.New("pp-app.json: engines.sdk is required and must be a positive integer")
	}
	if f.Server.Package == "" {
		return Manifest{}, errors.New("pp-app.json: server.package is required")
	}
	if f.Web.Package == "" {
		return Manifest{}, errors.New("pp-app.json: web.package is required")
	}
	m := Manifest{
		ID: f.ID, Name: f.Name, Version: f.Version, Description: f.Description,
		Author: f.Author, License: f.License, Homepage: f.Homepage, SDK: *f.Engines.SDK,
	}
	seen := map[string]bool{}
	for _, k := range f.NodeKinds {
		if !strings.HasPrefix(k.Kind, f.ID+".") || len(k.Kind) == len(f.ID)+1 {
			return Manifest{}, fmt.Errorf("pp-app.json: node kind %q must be %q<name>", k.Kind, f.ID+".")
		}
		if seen[k.Kind] {
			return Manifest{}, fmt.Errorf("pp-app.json: node kind %q listed twice", k.Kind)
		}
		seen[k.Kind] = true
		m.NodeKinds = append(m.NodeKinds, NodeKind{Kind: k.Kind})
	}
	return m, nil
}

// MustManifest is ParseManifest for an embedded manifest: it panics, since a
// broken pp-app.json is a build error, not a runtime condition.
func MustManifest(data []byte) Manifest {
	m, err := ParseManifest(data)
	if err != nil {
		panic(err)
	}
	return m
}
