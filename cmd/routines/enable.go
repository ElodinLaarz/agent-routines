package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newEnableCmd returns either `enable` or `disable` depending on `enable`.
func newEnableCmd(enable bool) *cobra.Command {
	verb, flag := "enable", true
	if !enable {
		verb, flag = "disable", false
	}
	return &cobra.Command{
		Use:   verb + " <name>",
		Short: verb + " a routine by flipping its `enabled:` field",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, _, err := loadCfg()
			if err != nil {
				return err
			}
			path := filepath.Join(cfg.RoutinesDir, name+".yaml")
			if _, err := os.Stat(path); err != nil {
				// also try .yml
				alt := filepath.Join(cfg.RoutinesDir, name+".yml")
				if _, err2 := os.Stat(alt); err2 == nil {
					path = alt
				} else {
					return fmt.Errorf("routine %q not found", name)
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var node yaml.Node
			if err := yaml.Unmarshal(data, &node); err != nil {
				return err
			}
			if err := setBoolField(&node, "enabled", flag); err != nil {
				return err
			}
			out, err := yaml.Marshal(&node)
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, out, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%sd %s\n", verb, name)
			return nil
		},
	}
}

// setBoolField finds (or creates) a top-level mapping key and sets it to bool b.
func setBoolField(root *yaml.Node, key string, b bool) error {
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("yaml: expected document node")
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return fmt.Errorf("yaml: expected top-level mapping")
	}
	val := "true"
	if !b {
		val = "false"
	}
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1].Tag = "!!bool"
			m.Content[i+1].Value = val
			return nil
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: val},
	)
	return nil
}
