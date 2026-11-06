ALTER TABLE attachment
  ADD COLUMN width INTEGER CHECK (width > 0),
  ADD COLUMN height INTEGER CHECK (height > 0);
