-- Links from a card to nodes in the drive (M46). Which node a card points at
-- is the card's business; who may see its name is the viewer's: the link is
-- re-resolved against each viewer's access when the board is read.
CREATE TABLE card_links (
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    node_id UUID NOT NULL REFERENCES public.nodes(id) ON DELETE CASCADE,
    PRIMARY KEY (card_id, node_id)
);
CREATE INDEX card_links_by_node ON card_links (node_id);
