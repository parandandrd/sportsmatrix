package main

import (
	"reflect"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"

	"github.com/parandandrd/sportsmatrix/internal/board"
	"github.com/parandandrd/sportsmatrix/internal/config"
)

// addSection records that boards were built from the config section field
// points at, as in &r.config.NHLConfig. A league's stats and headlines boards
// come from its section too, which is what lets the web UI group them.
func (r *rootArgs) addSection(field any, boards []board.Board) {
	if r.boardSections == nil {
		r.boardSections = make(map[board.Board]string)
	}

	key := configKey(r.config, field)
	for _, b := range boards {
		r.boardSections[b] = key
	}
}

// configKey returns the config file key for the field of c that field points
// at. Naming a section by its field rather than a string means it cannot be
// misspelled, or drift from the key the config is actually read from.
func configKey(c *config.Config, field any) string {
	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		if v.Field(i).Addr().Interface() == field {
			key, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
			return key
		}
	}

	return ""
}

// configSections lists the top-level keys of a YAML config in the order the
// file has them. The config itself is read into a struct, which keeps no order.
func configSections(dat []byte) ([]string, error) {
	var doc yamlv3.Node
	if err := yamlv3.Unmarshal(dat, &doc); err != nil {
		return nil, err
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yamlv3.MappingNode {
		return nil, nil
	}

	root := doc.Content[0]
	keys := make([]string, 0, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		keys = append(keys, root.Content[i].Value)
	}

	return keys, nil
}
