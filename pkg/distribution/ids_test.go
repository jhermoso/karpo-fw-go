package distribution_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

func TestIDsFromQuery(t *testing.T) {
	for _, c := range []struct {
		query   string
		raw     []string
		present bool
	}{
		{"name=x", nil, false},
		{"ids=", []string{""}, true},
		{"ids=a,b", []string{"a,b"}, true},
		{"ids=a&ids=b", []string{"a", "b"}, true},
	} {
		v, _ := url.ParseQuery(c.query)
		raw, present := distribution.IDsFromQuery(v)
		if present != c.present || !reflect.DeepEqual(raw, c.raw) {
			t.Errorf("%s: got %v %v", c.query, raw, present)
		}
	}
}

func filterOf(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestIDsFromFilter(t *testing.T) {
	for _, c := range []struct {
		body    string
		raw     []string
		present bool
		typeErr bool
	}{
		{`{"name":"x"}`, nil, false, false},
		{`{"ids":null}`, nil, false, false},
		{`{"ids":[]}`, []string{}, true, false},
		{`{"ids":["a","b"]}`, []string{"a", "b"}, true, false},
		{`{"ids":"a,b"}`, []string{"a,b"}, true, false},
		{`{"ids":["a",1]}`, nil, false, true},
		{`{"ids":7}`, nil, false, true},
	} {
		f := filterOf(t, c.body)
		raw, present, err := distribution.IDsFromFilter(f)
		var ve *domain.ValidationError
		if c.typeErr {
			if !errors.As(err, &ve) || ve.Errors[0].Field != "ids" || ve.Errors[0].Code != "type" {
				t.Errorf("%s: want ids/type, got %v", c.body, err)
			}
		} else if err != nil || present != c.present || !reflect.DeepEqual(raw, c.raw) {
			t.Errorf("%s: got %v %v %v", c.body, raw, present, err)
		}
		if _, still := f["ids"]; still {
			t.Errorf("%s: ids left in the filter", c.body)
		}
	}
}
