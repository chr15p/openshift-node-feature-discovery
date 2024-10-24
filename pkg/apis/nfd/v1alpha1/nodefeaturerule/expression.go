/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nodefeaturerule

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	strings "strings"

	"golang.org/x/exp/maps"
	"k8s.io/klog/v2"

	nfdv1alpha1 "github.com/openshift/node-feature-discovery/pkg/apis/nfd/v1alpha1"
)

var matchOps = map[nfdv1alpha1.MatchOp]struct{}{
	nfdv1alpha1.MatchAny:          {},
	nfdv1alpha1.MatchIn:           {},
	nfdv1alpha1.MatchNotIn:        {},
	nfdv1alpha1.MatchInRegexp:     {},
	nfdv1alpha1.MatchExists:       {},
	nfdv1alpha1.MatchDoesNotExist: {},
	nfdv1alpha1.MatchGt:           {},
	nfdv1alpha1.MatchLt:           {},
	nfdv1alpha1.MatchGtLt:         {},
	nfdv1alpha1.MatchIsTrue:       {},
	nfdv1alpha1.MatchIsFalse:      {},
}

// evaluateMatchExpression evaluates the MatchExpression against a single input value.
func evaluateMatchExpression(m *nfdv1alpha1.MatchExpression, valid bool, value interface{}) (bool, error) {
	if _, ok := matchOps[m.Op]; !ok {
		return false, fmt.Errorf("invalid Op %q", m.Op)
	}

	switch m.Op {
	case nfdv1alpha1.MatchAny:
		if len(m.Value) != 0 {
			return false, fmt.Errorf("invalid expression, 'value' field must be empty for Op %q (have %v)", m.Op, m.Value)
		}
		return true, nil
	case nfdv1alpha1.MatchExists:
		if len(m.Value) != 0 {
			return false, fmt.Errorf("invalid expression, 'value' field must be empty for Op %q (have %v)", m.Op, m.Value)
		}
		return valid, nil
	case nfdv1alpha1.MatchDoesNotExist:
		if len(m.Value) != 0 {
			return false, fmt.Errorf("invalid expression, 'value' field must be empty for Op %q (have %v)", m.Op, m.Value)
		}
		return !valid, nil
	}

	if valid {
		value := fmt.Sprintf("%v", value)
		switch m.Op {
		case nfdv1alpha1.MatchIn:
			if len(m.Value) == 0 {
				return false, fmt.Errorf("invalid expression, 'value' field must be non-empty for Op %q", m.Op)
			}
			for _, v := range m.Value {
				if value == v {
					return true, nil
				}
			}
		case nfdv1alpha1.MatchNotIn:
			if len(m.Value) == 0 {
				return false, fmt.Errorf("invalid expression, 'value' field must be non-empty for Op %q", m.Op)
			}
			for _, v := range m.Value {
				if value == v {
					return false, nil
				}
			}
			return true, nil
		case nfdv1alpha1.MatchInRegexp:
			if len(m.Value) == 0 {
				return false, fmt.Errorf("invalid expression, 'value' field must be non-empty for Op %q", m.Op)
			}
			valueRe := make([]*regexp.Regexp, len(m.Value))
			for i, v := range m.Value {
				re, err := regexp.Compile(v)
				if err != nil {
					return false, fmt.Errorf("invalid expressiom, 'value' field must only contain valid regexps for Op %q (have %v)", m.Op, m.Value)
				}
				valueRe[i] = re
			}
			for _, re := range valueRe {
				if re.MatchString(value) {
					return true, nil
				}
			}
		case nfdv1alpha1.MatchGt, nfdv1alpha1.MatchLt:
			if len(m.Value) != 1 {
				return false, fmt.Errorf("invalid expression, 'value' field must contain exactly one element for Op %q (have %v)", m.Op, m.Value)
			}

			l, err := strconv.Atoi(value)
			if err != nil {
				return false, fmt.Errorf("not a number %q", value)
			}
			r, err := strconv.Atoi(m.Value[0])
			if err != nil {
				return false, fmt.Errorf("not a number %q in %v", m.Value[0], m)
			}

			if (l < r && m.Op == nfdv1alpha1.MatchLt) || (l > r && m.Op == nfdv1alpha1.MatchGt) {
				return true, nil
			}
		case nfdv1alpha1.MatchGtLt:
			if len(m.Value) != 2 {
				return false, fmt.Errorf("invalid expression, value' field must contain exactly two elements for Op %q (have %v)", m.Op, m.Value)
			}
			v, err := strconv.Atoi(value)
			if err != nil {
				return false, fmt.Errorf("not a number %q", value)
			}
			lr := make([]int, 2)
			for i := 0; i < 2; i++ {
				lr[i], err = strconv.Atoi(m.Value[i])
				if err != nil {
					return false, fmt.Errorf("not a number %q in %v", m.Value[i], m)
				}
			}
			if lr[0] >= lr[1] {
				return false, fmt.Errorf("invalid expression, value[0] must be less than Value[1] for Op %q (have %v)", m.Op, m.Value)
			}
			return v > lr[0] && v < lr[1], nil
		case nfdv1alpha1.MatchIsTrue:
			if len(m.Value) != 0 {
				return false, fmt.Errorf("invalid expression, 'value' field must be empty for Op %q (have %v)", m.Op, m.Value)
			}
			return value == "true", nil
		case nfdv1alpha1.MatchIsFalse:
			if len(m.Value) != 0 {
				return false, fmt.Errorf("invalid expression, 'value' field must be empty for Op %q (have %v)", m.Op, m.Value)
			}
			return value == "false", nil
		default:
			return false, fmt.Errorf("unsupported Op %q", m.Op)
		}
	}
	return false, nil
}

// evaluateMatchExpressionKeys evaluates the MatchExpression against a set of keys.
func evaluateMatchExpressionKeys(m *nfdv1alpha1.MatchExpression, name string, keys map[string]nfdv1alpha1.Nil) (bool, error) {
	matched := false

	_, ok := keys[name]
	switch m.Op {
	case nfdv1alpha1.MatchAny:
		matched = true
	case nfdv1alpha1.MatchExists:
		matched = ok
	case nfdv1alpha1.MatchDoesNotExist:
		matched = !ok
	default:
		return false, fmt.Errorf("invalid Op %q when matching keys", m.Op)
	}

	if klogV := klog.V(3); klogV.Enabled() {
		klogV.InfoS("matched keys", "matchResult", matched, "matchKey", name, "matchOp", m.Op)
	} else if klogV := klog.V(4); klogV.Enabled() {
		k := maps.Keys(keys)
		sort.Strings(k)
		klogV.InfoS("matched keys", "matchResult", matched, "matchKey", name, "matchOp", m.Op, "inputKeys", k)
	}
	return matched, nil
}

// evaluateMatchExpressionValues evaluates the MatchExpression against a set of key-value pairs.
func evaluateMatchExpressionValues(m *nfdv1alpha1.MatchExpression, name string, values map[string]string) (bool, error) {
	v, ok := values[name]
	matched, err := evaluateMatchExpression(m, ok, v)
	if err != nil {
		return false, err
	}

	if klogV := klog.V(3); klogV.Enabled() {
		klogV.InfoS("matched values", "matchResult", matched, "matchKey", name, "matchOp", m.Op, "matchValue", m.Value)
	} else if klogV := klog.V(4); klogV.Enabled() {
		klogV.InfoS("matched values", "matchResult", matched, "matchKey", name, "matchOp", m.Op, "matchValue", m.Value, "inputValues", values)
	}

	return matched, nil
}

// MatchKeyNames evaluates the MatchExpression against names of a set of key features.
func MatchKeyNames(m *nfdv1alpha1.MatchExpression, keys map[string]nfdv1alpha1.Nil) (bool, []MatchedElement, error) {
	ret := []MatchedElement{}

	for k := range keys {
		if match, err := evaluateMatchExpression(m, true, k); err != nil {
			return false, nil, err
		} else if match {
			ret = append(ret, MatchedElement{"Name": k})
		}
	}
	// Sort for reproducible output
	sort.Slice(ret, func(i, j int) bool { return ret[i]["Name"] < ret[j]["Name"] })

	if klogV3 := klog.V(3); klogV3.Enabled() {
		mk := make([]string, len(ret))
		for i, v := range ret {
			mk[i] = v["Name"]
		}
		mkMsg := strings.Join(mk, ", ")

		if klogV4 := klog.V(4); klogV4.Enabled() {
			k := make([]string, 0, len(keys))
			for n := range keys {
				k = append(k, n)
			}
			sort.Strings(k)
			klogV3.InfoS("matched (key) names", "matchResult", mkMsg, "matchOp", m.Op, "matchValue", m.Value, "inputKeys", k)
		} else {
			klogV3.InfoS("matched (key) names", "matchResult", mkMsg, "matchOp", m.Op, "matchValue", m.Value)
		}
	}

	return len(ret) > 0, ret, nil
}

// MatchValueNames evaluates the MatchExpression against names of a set of value features.
func MatchValueNames(m *nfdv1alpha1.MatchExpression, values map[string]string) (bool, []MatchedElement, error) {
	ret := []MatchedElement{}

	for k, v := range values {
		if match, err := evaluateMatchExpression(m, true, k); err != nil {
			return false, nil, err
		} else if match {
			ret = append(ret, MatchedElement{"Name": k, "Value": v})
		}
	}
	// Sort for reproducible output
	sort.Slice(ret, func(i, j int) bool { return ret[i]["Name"] < ret[j]["Name"] })

	if klogV3 := klog.V(3); klogV3.Enabled() {
		mk := make([]string, len(ret))
		for i, v := range ret {
			mk[i] = v["Name"]
		}
		mkMsg := strings.Join(mk, ", ")

		if klogV4 := klog.V(4); klogV4.Enabled() {
			klogV3.InfoS("matched (value) names", "matchResult", mkMsg, "matchOp", m.Op, "matchValue", m.Value, "inputValues", values)
		} else {
			klogV3.InfoS("matched (value) names", "matchResult", mkMsg, "matchOp", m.Op, "matchValue", m.Value)
		}
	}

	return len(ret) > 0, ret, nil
}

// MatchInstanceAttributeNames evaluates the MatchExpression against a set of
// instance features, matching against the names of their attributes.
func MatchInstanceAttributeNames(m *nfdv1alpha1.MatchExpression, instances []nfdv1alpha1.InstanceFeature) ([]MatchedElement, error) {
	ret := []MatchedElement{}

	for _, i := range instances {
		if match, _, err := MatchValueNames(m, i.Attributes); err != nil {
			return nil, err
		} else if match {
			ret = append(ret, i.Attributes)
		}
	}
	return ret, nil
}

// MatchKeys evaluates the MatchExpressionSet against a set of keys.
func MatchKeys(m *nfdv1alpha1.MatchExpressionSet, keys map[string]nfdv1alpha1.Nil) (bool, error) {
	matched, _, err := MatchGetKeys(m, keys)
	return matched, err
}

// MatchedElement holds one matched Instance.
type MatchedElement map[string]string

// MatchGetKeys evaluates the MatchExpressionSet against a set of keys and
// returns all matched keys or nil if no match was found. Note that an empty
// MatchExpressionSet returns a match with an empty slice of matched features.
func MatchGetKeys(m *nfdv1alpha1.MatchExpressionSet, keys map[string]nfdv1alpha1.Nil) (bool, []MatchedElement, error) {
	ret := make([]MatchedElement, 0, len(*m))

	for n, e := range *m {
		match, err := evaluateMatchExpressionKeys(e, n, keys)
		if err != nil {
			return false, nil, err
		}
		if !match {
			return false, nil, nil
		}
		ret = append(ret, MatchedElement{"Name": n})
	}
	// Sort for reproducible output
	sort.Slice(ret, func(i, j int) bool { return ret[i]["Name"] < ret[j]["Name"] })
	return true, ret, nil
}

// MatchValues evaluates the MatchExpressionSet against a set of key-value pairs.
func MatchValues(m *nfdv1alpha1.MatchExpressionSet, values map[string]string) (bool, error) {
	matched, _, err := MatchGetValues(m, values)
	return matched, err
}

// MatchGetValues evaluates the MatchExpressionSet against a set of key-value
// pairs and returns all matched key-value pairs. Note that an empty
// MatchExpressionSet returns a match with an empty slice of matched features.
func MatchGetValues(m *nfdv1alpha1.MatchExpressionSet, values map[string]string) (bool, []MatchedElement, error) {
	ret := make([]MatchedElement, 0, len(*m))

	for n, e := range *m {
		match, err := evaluateMatchExpressionValues(e, n, values)
		if err != nil {
			return false, nil, err
		}
		if !match {
			return false, nil, nil
		}
		ret = append(ret, MatchedElement{"Name": n, "Value": values[n]})
	}
	// Sort for reproducible output
	sort.Slice(ret, func(i, j int) bool { return ret[i]["Name"] < ret[j]["Name"] })
	return true, ret, nil
}

// MatchInstances evaluates the MatchExpressionSet against a set of instance
// features, each of which is an individual set of key-value pairs
// (attributes).
func MatchInstances(m *nfdv1alpha1.MatchExpressionSet, instances []nfdv1alpha1.InstanceFeature) (bool, error) {
	v, err := MatchGetInstances(m, instances)
	return len(v) > 0, err
}

// MatchGetInstances evaluates the MatchExpressionSet against a set of instance
// features, each of which is an individual set of key-value pairs
// (attributes). A slice containing all matching instances is returned. An
// empty (non-nil) slice is returned if no matching instances were found.
func MatchGetInstances(m *nfdv1alpha1.MatchExpressionSet, instances []nfdv1alpha1.InstanceFeature) ([]MatchedElement, error) {
	ret := []MatchedElement{}

	for _, i := range instances {
		if match, err := MatchValues(m, i.Attributes); err != nil {
			return nil, err
		} else if match {
			ret = append(ret, i.Attributes)
		}
	}
	return ret, nil
}
