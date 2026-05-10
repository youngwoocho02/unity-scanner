package format

import (
	"reflect"
	"testing"
)

func TestCompressNames(t *testing.T) {
	got := CompressNames([]string{
		"Recipe_003",
		"Recipe_001",
		"Recipe_002",
		"Blender",
		"Stove_01",
		"Stove_02",
	})

	want := []string{"Blender", "Recipe_001..003", "Stove_01", "Stove_02"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
