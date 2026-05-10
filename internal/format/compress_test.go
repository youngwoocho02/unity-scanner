package format

import (
	"fmt"
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

func TestSplitTrailingNumber(t *testing.T) {
	prefix, num, width, ok := splitTrailingNumber("Recipe_001")
	if !ok || prefix != "Recipe_" || num != 1 || width != 3 {
		t.Fatalf("prefix=%q num=%d width=%d ok=%v", prefix, num, width, ok)
	}
	if _, _, _, ok := splitTrailingNumber("Recipe"); ok {
		t.Fatal("plain name parsed as numbered")
	}
}

func BenchmarkCompressNamesNumbered(b *testing.B) {
	names := make([]string, 0, 2000)
	for i := 0; i < 2000; i++ {
		names = append(names, fmt.Sprintf("Item_%04d", i))
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out := CompressNames(names)
		if len(out) != 1 {
			b.Fatalf("out=%d", len(out))
		}
	}
}
