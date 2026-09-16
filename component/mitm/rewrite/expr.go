package rewrite

import (
	"reflect"
	"strings"
)

// Env is the expr program environment for JSON rewrite rules. An expr
// program runs against a single JSON object exposed as the field data,
// e.g. `data.foo.bar == 1 || Delete("data.foo")`.
type Env struct {
	Data map[string]any `expr:"data"`
}

// Assign sets data.<key> to value, creating intermediate objects. An
// existing value must have the same reflect.Kind as value.
func (e Env) Assign(key string, value any) bool {
	after, _ := strings.CutPrefix(key, "data.")
	keys := strings.Split(after, ".")
	data := e.Data
	for i, l := 0, len(keys); i < l; i++ {
		k := keys[i]
		if i == l-1 {
			if v, ok := data[k]; ok && reflect.ValueOf(v).Kind() != reflect.ValueOf(value).Kind() {
				return false
			}
			data[k] = value
			return true
		}
		if sub, ok := data[k].(map[string]any); ok {
			data = sub
		} else {
			return false
		}
	}
	return false
}

// delete removes data.<key> and reports whether the key existed.
func (e Env) delete(key string) bool {
	after, _ := strings.CutPrefix(key, "data.")
	keys := strings.Split(after, ".")
	data := e.Data
	for i, l := 0, len(keys); i < l; i++ {
		k := keys[i]
		if i == l-1 {
			if _, ok := data[k]; ok {
				delete(data, k)
				return true
			}
			return false
		}
		if sub, ok := data[k].(map[string]any); ok {
			data = sub
		} else {
			return false
		}
	}
	return false
}

// Delete removes the semicolon-separated data.<key> paths and reports
// whether at least one existed.
func (e Env) Delete(keys string) bool {
	var v bool
	for _, k := range strings.Split(keys, ";") {
		v = e.delete(k) || v
	}
	return v
}

// DeleteArray removes elements from the data.<key> array whose field key1
// equals value (a string value is split on ";;" into an any-of list) and
// reports whether any element was removed.
func (e Env) DeleteArray(key string, key1 string, value any) bool {
	after, _ := strings.CutPrefix(key, "data.")
	keys := strings.Split(after, ".")
	data := e.Data
	var vss []string
	if s, ok := value.(string); ok {
		vss = strings.Split(s, ";;")
	}
	for i, l := 0, len(keys); i < l; i++ {
		k := keys[i]
		if i == l-1 {
			arr, ok := data[k].([]any)
			if !ok {
				return false
			}
			tmp := make([]any, 0, len(arr))
			for _, v := range arr {
				m, ok := v.(map[string]any)
				if !ok {
					continue
				}
				if len(vss) != 0 {
					keep := true
					for _, s := range vss {
						keep = keep && m[key1] != s
					}
					if keep {
						tmp = append(tmp, m)
					}
				} else if m[key1] != value {
					tmp = append(tmp, m)
				}
			}
			if len(tmp) != 0 {
				data[k] = tmp
				return true
			}
			return false
		}
		if sub, ok := data[k].(map[string]any); ok {
			data = sub
		} else {
			return false
		}
	}
	return false
}
