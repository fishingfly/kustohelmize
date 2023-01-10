package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/yeahdongcn/kustohelmize/pkg/chart"
	"github.com/yeahdongcn/kustohelmize/pkg/util"
	"gopkg.in/yaml.v2"
)

type KeyType string

const (
	KeyTypeFile     KeyType = "file"
	KeyTypeBuiltIn  KeyType = "builtin"
	KeyTypeShared   KeyType = "shared"
	KeyTypeHelpers  KeyType = "helpers"
	KeyTypeNotFound KeyType = "notfound"
)

func (t KeyType) IsHelpersType() bool {
	return t == KeyTypeHelpers
}

type XPathStrategy string

const (
	XPathStrategyInline        XPathStrategy = "inline"
	XPathStrategyInlineYAML    XPathStrategy = "inline-yaml"
	XPathStrategyNewline       XPathStrategy = "newline"
	XPathStrategyNewlineYAML   XPathStrategy = "newline-yaml"
	XPathStrategyControlIf     XPathStrategy = "control-if"
	XPathStrategyControlIfYAML XPathStrategy = "control-if-yaml"
	XPathStrategyControlWith   XPathStrategy = "control-with"
	XPathStrategyControlRange  XPathStrategy = "control-range"
)

type XPathConfig struct {
	Strategy     XPathStrategy `yaml:"strategy"`
	Key          string        `yaml:"key"`
	Value        interface{}   `yaml:"value,omitempty"`
	DefaultValue interface{}   `yaml:"defaultValue,omitempty"`
}

type XPathConfigs []XPathConfig

type XPath string

func (xpath XPath) IsRoot() bool {
	return xpath == XPathRoot
}

func (xpath XPath) NewChild(s string, sliceIndex int) XPath {
	if xpath.IsRoot() {
		return XPath(s)
	}
	if sliceIndex == XPathSliceIndexNone {
		return XPath(fmt.Sprintf("%s.%s", xpath, s))
	}
	return XPath(fmt.Sprintf("%s[%d].%s", xpath, sliceIndex, s))
}

type Config map[XPath]XPathConfigs

func defaultGlobalConfig(chartname string) Config {
	return Config{
		"metadata.name": []XPathConfig{
			{
				Strategy: XPathStrategyInline,
				Key:      fmt.Sprintf(chart.FullNameFormat, chartname),
			},
		},
		"metadata.labels": []XPathConfig{
			{
				Strategy: XPathStrategyNewline,
				Key:      fmt.Sprintf(chart.CommonLabelsFormat, chartname),
			},
		},
	}
}

type GenericMap map[string]interface{}

func defaultSharedValues() GenericMap {
	return GenericMap{
		"resources":          GenericMap{},
		"nodeSelector":       GenericMap{},
		"tolerations":        GenericMap{},
		"affinity":           GenericMap{},
		"podSecurityContext": GenericMap{},
		"securityContext":    GenericMap{},
	}
}

type ChartConfig struct {
	Logger       logr.Logger
	Chartname    string            `yaml:"chartname"`
	SharedValues GenericMap        `yaml:"sharedValues"`
	GlobalConfig Config            `yaml:"globalConfig"`
	FileConfig   map[string]Config `yaml:"fileConfig"`
}

func NewChartConfig(logger logr.Logger, chartname string) *ChartConfig {
	config := &ChartConfig{
		Logger:       logger,
		Chartname:    chartname,
		SharedValues: defaultSharedValues(),
		GlobalConfig: defaultGlobalConfig(chartname),
		FileConfig:   map[string]Config{},
	}
	return config
}

func (cc *ChartConfig) Values() (string, error) {
	str := ""
	var err error
	var out []byte
	// 1. SharedValues
	if len(cc.SharedValues) > 0 {
		out, err := yaml.Marshal(cc.SharedValues)
		if err != nil {
			return str, err
		}
		str += fmt.Sprintf("%s\n", string(out))
	}

	// 2. FileConfig
	root := GenericMap{}
	for filename, fileConfig := range cc.FileConfig {
		key := util.LowerCamelFilenameWithoutExt(filepath.Base(filename))
		root[key] = GenericMap{}
		fileRoot := root[key].(GenericMap)

		for _, v := range fileConfig {
			for _, c := range v {
				configRoot := fileRoot
				substrings := strings.Split(c.Key, XPathSeparator)
				for i, substring := range substrings {
					// XXX: For shared values and global defined values, we should not extend values.yaml
					if i == 0 && (substring == sharedValuesPrefix || substring == cc.Chartname || substring == "") {
						break
					}
					if i == 1 && substring == "Chart" {
						break
					}
					if configRoot[substring] == nil {
						configRoot[substring] = GenericMap{}
					}
					if i < len(substrings)-1 {
						configRoot = configRoot[substring].(GenericMap)
					} else {
						if c.Value == nil {
							cc.Logger.Info(fmt.Sprintf("%s: %s", c.Key, "nil"))
							delete(configRoot, substring)
						} else {
							switch v := c.Value.(type) {
							case int:
								cc.Logger.V(10).Info("type int", "key", c.Key, "value", v)
								configRoot[substring] = v
							case string:
								cc.Logger.V(10).Info("type string", "key", c.Key, "value", v)
								configRoot[substring] = v
							case map[interface{}]interface{}:
								cc.Logger.V(10).Info("type map[interface{}]interface{}", "key", c.Key)
								if len(v) == 0 {
									delete(configRoot, substring)
								} else {
									configRoot[substring] = v
								}
							case []interface{}:
								cc.Logger.V(10).Info("type []interface{}", "key", c.Key)
								if len(v) == 0 {
									delete(configRoot, substring)
								} else {
									configRoot[substring] = v
								}
							default:
								cc.Logger.V(10).Info("type default", "key", c.Key)
								configRoot[substring] = v
							}
						}
					}
				}
			}
		}
	}
	out, err = yaml.Marshal(root)
	if err != nil {
		return str, nil
	}

	str += fmt.Sprintf("%s\n", string(out))
	return str, nil
}

func (c *ChartConfig) GetFormattedKeyWithDefaultValue(xc *XPathConfig, prefix string) (string, KeyType) {
	key, keyType := c.getKey(xc)
	if keyType == KeyTypeFile {
		key = fmt.Sprintf(".Values.%s.%s", prefix, key)
	} else if keyType == KeyTypeShared {
		key = fmt.Sprintf(".Values.%s", key)
	} else if keyType == KeyTypeNotFound {
		if xc.Strategy == XPathStrategyControlIf || xc.Strategy == XPathStrategyControlIfYAML {
			key = fmt.Sprintf(".Values.%s", key)
		} else {
			panic(fmt.Sprintf("%s not found", xc.Key))
		}
	}
	if xc.DefaultValue != nil {
		key = fmt.Sprintf("%s | default %s", key, xc.DefaultValue)
	}
	return key, keyType
}

func (c *ChartConfig) keyExist(key string) (string, bool) {
	substrings := strings.Split(key, XPathSeparator)
	if len(substrings) <= 1 {
		return key, false
	}
	key = key[len(sharedValuesPrefix)+len(XPathSeparator):]
	current := c.SharedValues
	for index, substring := range substrings {
		if substring == sharedValuesPrefix {
			continue
		}
		out, err := yaml.Marshal(current[substring])
		if err != nil {
			return key, false
		}
		next := GenericMap{}
		err = yaml.Unmarshal(out, &next)
		if err != nil {
			if index == len(substrings)-1 {
				continue
			}
			return key, false
		}
		if next == nil {
			return key, false
		} else {
			current = next
		}
	}
	return key, true
}

func (c *ChartConfig) getKey(xc *XPathConfig) (string, KeyType) {
	key := xc.Key
	if strings.HasPrefix(key, sharedValuesPrefix) {
		key, exist := c.keyExist(key)
		if !exist {
			return key, KeyTypeNotFound
		}
		return key, KeyTypeShared
	} else if strings.HasPrefix(key, c.Chartname) {
		return key, KeyTypeHelpers
	} else if strings.HasPrefix(key, builtInValuesPrefix) {
		return key, KeyTypeBuiltIn
	}
	return key, KeyTypeFile
}
