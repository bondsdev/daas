ALTER TABLE tenants ALTER COLUMN pos_search_mode SET DEFAULT 'both';

UPDATE tenants SET pos_search_mode = 'both' WHERE pos_search_mode = 'barcode';
