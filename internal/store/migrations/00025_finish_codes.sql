-- +goose Up
-- Price-book SKUs carry the finish as a suffix code (Brizo -PC, Barclay -ORB, Newport /26).
-- brand_code '*' is the generic fallback used only for brands without their own rows.
CREATE TABLE IF NOT EXISTS finish_codes (
  brand_code text NOT NULL,
  code       text NOT NULL,
  finish     text NOT NULL,
  PRIMARY KEY (brand_code, code)
);
INSERT INTO finish_codes (brand_code, code, finish) VALUES
-- Brizo
('BRZ','PC','Polished Chrome'),('BRZ','PN','Polished Nickel'),('BRZ','GL','Luxe Gold'),('BRZ','NK','Luxe Nickel'),
('BRZ','BL','Matte Black'),('BRZ','SL','Luxe Steel'),('BRZ','BNX','Brushed Nickel'),('BRZ','BN','Brushed Nickel'),
('BRZ','RB','Venetian Bronze'),('BRZ','PG','Polished Gold'),('BRZ','BLGL','Matte Black / Luxe Gold'),('BRZ','SS','Stainless'),
('BRZ','BLSL','Matte Black / Luxe Steel'),('BRZ','NKBL','Luxe Nickel / Matte Black'),('BRZ','ULB','Unlacquered Brass'),
('BRZ','GLTK','Luxe Gold / Teak'),('BRZ','PNTK','Polished Nickel / Teak'),('BRZ','NKTK','Luxe Nickel / Teak'),
-- Barclay
('BAR','CP','Polished Chrome'),('BAR','ORB','Oil Rubbed Bronze'),('BAR','BN','Brushed Nickel'),('BAR','PB','Polished Brass'),
('BAR','PN','Polished Nickel'),('BAR','WH','White'),('BAR','MB','Matte Black'),('BAR','SN','Satin Nickel'),('BAR','AB','Antique Brass'),
('BAR','BL','Black'),('BAR','SS','Stainless Steel'),('BAR','SB','Satin Brass'),('BAR','AC','Antique Copper'),('BAR','BQ','Bisque'),
-- California Faucets
('CAL','PC','Polished Chrome'),('CAL','SN','Satin Nickel'),('CAL','PN','Polished Nickel'),('CAL','ORB','Oil Rubbed Bronze'),
('CAL','PB','Polished Brass'),('CAL','SB','Satin Brass'),('CAL','BBU','Burnished Brass Uncoated'),('CAL','PBU','Polished Brass Uncoated'),
('CAL','BNU','Burnished Nickel Uncoated'),('CAL','ABF','Antique Brass Flat'),('CAL','MBLK','Matte Black'),('CAL','BLK','Matte Black'),
('CAL','MWHT','Matte White'),('CAL','WHT','Matte White'),('CAL','CB','Carbon'),('CAL','BTB','Bella Terra Bronze'),('CAL','ACF','Antique Copper Flat'),
('CAL','SC','Satin Chrome'),('CAL','LPG','Lifetime Polished Gold'),('CAL','LSG','Lifetime Satin Gold'),('CAL','SBZ','Satin Bronze'),('CAL','MOB','Mocha Bronze'),
-- Alno
('ALN','PC','Polished Chrome'),('ALN','SN','Satin Nickel'),('ALN','PN','Polished Nickel'),('ALN','BRZ','Bronze'),('ALN','PB','Polished Brass'),
('ALN','MB','Matte Black'),('ALN','SB','Satin Brass'),('ALN','PA','Polished Antique'),('ALN','MN','Matte Nickel'),('ALN','CHBRZ','Chocolate Bronze'),
-- Artos
('ART','CH','Chrome'),('ART','BK','Matte Black'),('ART','PN','Polished Nickel'),('ART','BN','Brushed Nickel'),('ART','WH','White'),('ART','BG','Brushed Gold'),
-- Cheviot
('CHV','CH','Chrome'),('CHV','BN','Brushed Nickel'),('CHV','WH','White'),('CHV','AB','Antique Bronze'),('CHV','PN','Polished Nickel'),
('CHV','BK','Matte Black'),('CHV','SB','Satin Brass'),('CHV','MB','Matte Black'),('CHV','PB','Polished Brass'),('CHV','BC','Brushed Chrome'),
-- Baci Mirrors
('BAC','SN','Satin Nickel'),('BAC','BZ','Bronze'),('BAC','CHR','Chrome'),('BAC','PN','Polished Nickel'),('BAC','PB','Polished Brass'),('BAC','MB','Matte Black'),
-- Newport Brass
('NEW','26','Polished Chrome'),('NEW','15','Satin Nickel'),('NEW','15S','Satin Nickel PVD'),('NEW','15A','Antique Nickel'),('NEW','04','Satin Brass'),
('NEW','01','Forever Brass PVD'),('NEW','03N','Polished Brass Uncoated'),('NEW','10B','Oil Rubbed Bronze'),('NEW','20','Stainless Steel PVD'),
('NEW','50','White'),('NEW','52','Matte Black'),('NEW','24','Satin Gold PVD'),('NEW','24A','French Gold PVD'),('NEW','07','English Bronze'),
('NEW','06','Antique Brass'),('NEW','08A','Antique Copper'),('NEW','10','Satin Bronze PVD'),('NEW','034','Satin Brass PVD'),('NEW','03W','Polished Brass PVD'),
('NEW','56','Polished Nickel Uncoated'),('NEW','65','Biscuit'),('NEW','14','Polished Nickel'),('NEW','08','Polished Copper'),
-- Kohler
('KOL','CP','Polished Chrome'),('KOL','BN','Vibrant Brushed Nickel'),('KOL','2BZ','Oil-Rubbed Bronze'),('KOL','BL','Matte Black'),('KOL','SN','Vibrant Polished Nickel'),
('KOL','2MB','Vibrant Brushed Moderne Brass'),('KOL','BV','Vibrant Brushed Bronze'),('KOL','BGD','Vibrant Brushed Gold'),('KOL','AF','Vibrant French Gold'),
('KOL','G','Brushed Chrome'),('KOL','RGD','Vibrant Rose Gold'),('KOL','0','White'),('KOL','96','Biscuit'),('KOL','7','Black Black'),('KOL','47','Almond'),
('KOL','95','Ice Grey'),('KOL','NY','Dune'),('KOL','TT','Titanium'),('KOL','2BL','Matte Black'),('KOL','SNC','Vibrant Polished Nickel'),
-- Graff
('GRF','PC','Polished Chrome'),('GRF','PN','Polished Nickel'),('GRF','BN','Brushed Nickel'),('GRF','SN','Steelnox Satin Nickel'),('GRF','OB','Olive Bronze'),
('GRF','MBK','Matte Black'),('GRF','MBW','Matte White'),('GRF','BB','Brushed Brass'),('GRF','PB','Polished Brass'),('GRF','AU','24K Gold'),
('GRF','BAU','Brushed Gold'),('GRF','RG','Rose Gold'),('GRF','GM','Gunmetal'),('GRF','BGM','Brushed Gunmetal'),('GRF','UB','Unfinished Brass'),
('GRF','OX','Onyx'),('GRF','SB','Satin Brass'),('GRF','VB','Vintage Brushed Brass'),
-- Rohl
('ROL','APC','Polished Chrome'),('ROL','PN','Polished Nickel'),('ROL','STN','Satin Nickel'),('ROL','TCB','Tuscan Brass'),('ROL','IB','Inca Brass'),
('ROL','EB','English Bronze'),('ROL','MB','Matte Black'),('ROL','ULB','Unlacquered Brass'),('ROL','SG','Satin Gold'),('ROL','SE','Satin English Gold'),
('ROL','PG','Polished Gold'),('ROL','ORB','Oil Rubbed Bronze'),('ROL','TZ','Tuscan Bronze'),('ROL','IL','Italian Brass'),('ROL','BOR','Black Onyx'),
-- Baldwin
('BAL','003','Lifetime Polished Brass'),('BAL','033','Vintage Brass'),('BAL','040','Satin Brass'),('BAL','050','Antique Brass'),('BAL','055','Lifetime Polished Nickel'),
('BAL','056','Lifetime Satin Nickel'),('BAL','060','Satin Brass & Brown'),('BAL','102','Oil Rubbed Bronze'),('BAL','112','Venetian Bronze'),('BAL','150','Satin Nickel'),
('BAL','151','Antique Nickel'),('BAL','190','Satin Black'),('BAL','260','Polished Chrome'),('BAL','264','Satin Chrome'),('BAL','402','Distressed Oil Rubbed Bronze'),
('BAL','412','Distressed Venetian Bronze'),('BAL','452','Distressed Antique Nickel'),('BAL','031','Non-Lacquered Brass'),
-- MGS
('BGO','SSP','Polished Stainless Steel'),('BGO','SSM','Matte Stainless Steel'),('MGS','SSP','Polished Stainless Steel'),('MGS','SSM','Matte Stainless Steel'),
-- generic fallback for brands without their own rows
('*','PC','Polished Chrome'),('*','CP','Polished Chrome'),('*','CH','Chrome'),('*','CHR','Chrome'),('*','PN','Polished Nickel'),('*','SN','Satin Nickel'),
('*','BN','Brushed Nickel'),('*','ORB','Oil Rubbed Bronze'),('*','MB','Matte Black'),('*','MBK','Matte Black'),('*','BLK','Matte Black'),('*','PB','Polished Brass'),
('*','SB','Satin Brass'),('*','AB','Antique Brass'),('*','WH','White'),('*','WHT','White'),('*','SS','Stainless Steel'),('*','NK','Nickel'),('*','GL','Gold'),
('*','BZ','Bronze'),('*','BRZ','Bronze'),('*','PG','Polished Gold'),('*','SC','Satin Chrome'),('*','UB','Unlacquered Brass'),('*','ULB','Unlacquered Brass')
ON CONFLICT DO NOTHING;

-- families with no known finish get a marker so a finish search still finds them (ranked last)
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION refresh_family_blob() RETURNS void LANGUAGE sql AS $$
  UPDATE family_best fb SET blob = sub.blob
  FROM (
    SELECT f.id, concat_ws(' ', f.title, f.description, f.family_key, f.key_n, f.collection, b.name,
      f.product_type, array_to_string(f.category_path, ' '),
      (SELECT string_agg(DISTINCT p.sku || ' ' || COALESCE(p.sku_n, ''), ' ') FROM products p WHERE p.family_id = f.id),
      COALESCE((SELECT string_agg(DISTINCT p.finish, ' ') FROM products p WHERE p.family_id = f.id AND COALESCE(p.finish,'') <> ''), 'finish:any')) AS blob
    FROM product_families f JOIN brands b ON b.id = f.brand_id) sub
  WHERE fb.family_id = sub.id AND fb.blob IS DISTINCT FROM sub.blob;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS finish_codes;
