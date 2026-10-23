package deck

import "testing"

func TestNestedGroupWithoutExplicitParentIsAlwaysVisible(t *testing.T) {
	for i := 0; i < 100; i++ {
		roots := BuildGroupTree([]GroupRecord{{Path: "Product/Builder", Name: "Builder", Expanded: true}}, nil)
		if len(roots) != 1 || roots[0].Path != "Product" || len(roots[0].Children) != 1 {
			t.Fatalf("lost implicit parent: %+v", roots)
		}
	}
}
