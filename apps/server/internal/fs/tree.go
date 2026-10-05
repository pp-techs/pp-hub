package fs

import (
	"context"
	"fmt"

	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// TreeNode is one live node of a subtree as an export lays it out: a folder
// with its contents, a page (block document) with its sub-pages, or any other
// file.
type TreeNode struct {
	Node store.Node
	// Page marks a block document: a file whose current version is
	// markdown. Its Children are its live sub-pages (M43).
	Page     bool
	Children []*TreeNode
}

// Tree reads the live subtree under root — a folder, or a page with its
// sub-pages at any depth — for the zip and the export, which must know every
// page's place in the archive before writing the first one: a link between
// two of them becomes a relative path. Only nodes are read, never content.
// Nesting past maxTreeDepth is refused, as Copy refuses it.
func (s *Service) Tree(ctx context.Context, root store.Node) (*TreeNode, error) {
	t := &TreeNode{Node: root}
	switch root.Kind {
	case store.NodeKindDir:
	case store.NodeKindFile:
		ver, err := s.q.GetCurrentVersion(ctx, root.ID)
		if err != nil {
			return nil, err
		}
		if t.Page = IsMarkdown(ver.Mime); !t.Page {
			return t, nil
		}
	default:
		return t, nil
	}
	return t, s.treeChildren(ctx, t, 0)
}

func (s *Service) treeChildren(ctx context.Context, t *TreeNode, depth int) error {
	if depth >= maxTreeDepth {
		return fmt.Errorf("folder nesting exceeds %d levels", maxTreeDepth)
	}
	rows, err := s.q.ListTreeChildren(ctx, store.ListTreeChildrenParams{
		SpaceID:  t.Node.SpaceID,
		ParentID: t.Node.ID,
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		page := row.Node.Kind == store.NodeKindFile && IsMarkdown(row.Mime)
		// Under a page only sub-pages count, as ListChildPages has it.
		if t.Page && !page {
			continue
		}
		child := &TreeNode{Node: row.Node, Page: page}
		t.Children = append(t.Children, child)
		if row.Node.Kind == store.NodeKindDir || (page && row.HasPages) {
			if err := s.treeChildren(ctx, child, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
