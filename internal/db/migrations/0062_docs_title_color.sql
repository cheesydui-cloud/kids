-- Optional hex color for the document title in list / view / preview.
ALTER TABLE docs ADD COLUMN title_color TEXT NOT NULL DEFAULT '';
