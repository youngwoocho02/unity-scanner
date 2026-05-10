package format

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var trailingNumber = regexp.MustCompile(`^(.*?)(\d+)$`)

type numberName struct {
	raw    string
	prefix string
	num    int
	width  int
}

// CompressNames folds stable numbered sequences such as Recipe_001..018.
// It only compresses runs of three or more names so short lists stay explicit.
func CompressNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}

	seen := map[string]bool{}
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}
	sort.Strings(unique)

	numbered := make([]numberName, 0, len(unique))
	plain := make([]string, 0, len(unique))
	for _, name := range unique {
		m := trailingNumber.FindStringSubmatch(name)
		if m == nil {
			plain = append(plain, name)
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			plain = append(plain, name)
			continue
		}
		numbered = append(numbered, numberName{
			raw:    name,
			prefix: m[1],
			num:    n,
			width:  len(m[2]),
		})
	}

	sort.Slice(numbered, func(i, j int) bool {
		if numbered[i].prefix != numbered[j].prefix {
			return numbered[i].prefix < numbered[j].prefix
		}
		if numbered[i].width != numbered[j].width {
			return numbered[i].width < numbered[j].width
		}
		return numbered[i].num < numbered[j].num
	})

	out := make([]string, 0, len(unique))
	out = append(out, plain...)

	for i := 0; i < len(numbered); {
		j := i + 1
		for j < len(numbered) &&
			numbered[j].prefix == numbered[i].prefix &&
			numbered[j].width == numbered[i].width &&
			numbered[j].num == numbered[j-1].num+1 {
			j++
		}

		if j-i >= 3 {
			out = append(out, fmt.Sprintf("%s%0*d..%0*d",
				numbered[i].prefix,
				numbered[i].width,
				numbered[i].num,
				numbered[i].width,
				numbered[j-1].num,
			))
		} else {
			for k := i; k < j; k++ {
				out = append(out, numbered[k].raw)
			}
		}
		i = j
	}

	sort.Strings(out)
	return out
}

func Lines(items []string, perLine int) []string {
	if perLine <= 0 {
		perLine = 6
	}
	if len(items) == 0 {
		return nil
	}

	lines := make([]string, 0, (len(items)+perLine-1)/perLine)
	for i := 0; i < len(items); i += perLine {
		end := i + perLine
		if end > len(items) {
			end = len(items)
		}
		lines = append(lines, strings.Join(items[i:end], ", "))
	}
	return lines
}
