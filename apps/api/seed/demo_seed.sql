-- ============================================================================
-- DaaS — Demo seed data (Tienda Online de Cosméticos, Venezuela)
-- ============================================================================
-- Genera un dataset coherente y con volumen para demos a clientes y pruebas de
-- paginación / transacciones / flujos. Idempotente: hace RESET del catálogo y
-- las transacciones del tenant y vuelve a sembrar. MANTIENE: tenant, usuarios
-- reales (owner/employee), métodos de pago, features y branding. Los
-- motorizados demo (email LIKE 'motorizado.demo%') SÍ se recrean en cada
-- corrida — nunca toca un rider real con otro email.
--
-- Portable: resuelve tenant/owner/payment-methods dinámicamente, así el mismo
-- archivo corre en local y en prod.
--
-- Uso local:  psql "$DATABASE_URL" -f apps/api/seed/demo_seed.sql
-- Uso prod:   correr el contenido vía Supabase (SQL editor / MCP).
--
-- Triggers OFF (session_replication_role=replica) para controlar stock a mano.
-- Los CHECK y NOT NULL siguen activos: los valores generados los respetan.
-- ============================================================================

BEGIN;
SET session_replication_role = replica;

-- ---------- Contexto (resuelto por el email del owner, portable local/prod) ----------
-- Prod: el tenant real de danztty@gmail.com. Local: si ese email no existe
-- ahi (dev), cae al tenant de prueba Bond para que el script no quede sin
-- destino. Nunca hardcodear un tenant_id suelto otra vez: eso es lo que
-- rompia la portabilidad antes.
CREATE TEMP TABLE _tenant ON COMMIT DROP AS
SELECT COALESCE(
  (SELECT tenant_id FROM tenant_users WHERE email = 'danztty@gmail.com' ORDER BY (role = 'owner') DESC LIMIT 1),
  'e75c20d2-5131-4643-9b33-3065b8adbbce'::uuid
) AS tenant_id;

CREATE TEMP TABLE _ctx ON COMMIT DROP AS
SELECT
  tn.tenant_id,
  (SELECT id FROM tenant_users
     WHERE tenant_id = tn.tenant_id
     ORDER BY (role = 'owner') DESC LIMIT 1) AS owner_id,
  (SELECT id FROM tenant_payment_methods
     WHERE tenant_id = tn.tenant_id AND type = 'pago_movil'
     LIMIT 1) AS pm_pagomovil,
  (SELECT id FROM tenant_payment_methods
     WHERE tenant_id = tn.tenant_id AND type = 'zelle'
     LIMIT 1) AS pm_zelle
FROM _tenant tn;

-- Delivery necesita estar prendido para que los motorizados demo tengan sentido.
UPDATE tenants SET delivery_enabled = true WHERE id = (SELECT tenant_id FROM _ctx);

-- ============================== RESET ==============================
-- Orden FK-safe. Scopeado al tenant. NO toca tenants/users/payment_methods/features.
DELETE FROM fiscal_invoice_queue   WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM fiscal_invoice_lines   WHERE invoice_id IN (SELECT id FROM fiscal_invoices   WHERE tenant_id = (SELECT tenant_id FROM _ctx));
DELETE FROM internal_invoice_lines WHERE invoice_id IN (SELECT id FROM internal_invoices WHERE tenant_id = (SELECT tenant_id FROM _ctx));
DELETE FROM sales_order_lines      WHERE order_id   IN (SELECT id FROM sales_orders       WHERE tenant_id = (SELECT tenant_id FROM _ctx));
DELETE FROM shop_order_lines       WHERE order_id   IN (SELECT id FROM shop_orders        WHERE tenant_id = (SELECT tenant_id FROM _ctx));
DELETE FROM purchase_order_lines   WHERE po_id      IN (SELECT id FROM purchase_orders    WHERE tenant_id = (SELECT tenant_id FROM _ctx));
DELETE FROM sales_orders           WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM fiscal_invoices        WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM internal_invoices      WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM shop_orders            WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM riders                 WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM tenant_users           WHERE tenant_id = (SELECT tenant_id FROM _ctx) AND email LIKE 'motorizado.demo%';
DELETE FROM purchase_orders        WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM suppliers              WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM inventory_movements    WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM product_stock          WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM products               WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM product_categories     WHERE tenant_id = (SELECT tenant_id FROM _ctx);
DELETE FROM invoice_correlative_seq WHERE tenant_id = (SELECT tenant_id FROM _ctx);

-- ============================== CATEGORÍAS ==============================
-- Padres
INSERT INTO product_categories (id, tenant_id, name, parent_id)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), n, NULL
FROM (VALUES ('Maquillaje'),('Cuidado de la Piel'),('Cabello'),('Fragancias'),('Uñas'),('Accesorios')) v(n);

-- Hijos
INSERT INTO product_categories (id, tenant_id, name, parent_id)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), c.name,
       (SELECT id FROM product_categories WHERE tenant_id=(SELECT tenant_id FROM _ctx) AND name=c.parent)
FROM (VALUES
  ('Labiales','Maquillaje'),
  ('Rostro','Maquillaje'),
  ('Ojos','Maquillaje'),
  ('Cuidado Facial','Cuidado de la Piel'),
  ('Cuidado Corporal','Cuidado de la Piel'),
  ('Shampoo','Cabello'),
  ('Tratamientos','Cabello')
) c(name, parent);

-- ============================== PRODUCTOS (45) ==============================
INSERT INTO products (id, tenant_id, name, sku, barcode, description, category_id,
                      is_fiscal, fiscal_price, internal_price, tax_rate, active, created_at)
SELECT
  gen_random_uuid(),
  (SELECT tenant_id FROM _ctx),
  d.name, d.sku,
  '759' || lpad((row_number() OVER (ORDER BY d.sku))::text, 10, '0'),
  d.name || ' — presentación estándar, calidad premium.',
  (SELECT id FROM product_categories WHERE tenant_id=(SELECT tenant_id FROM _ctx) AND name=d.cat LIMIT 1),
  d.is_fiscal,
  CASE WHEN d.is_fiscal THEN round(d.price * 1.10, 2) ELSE NULL END,
  d.price,
  CASE WHEN d.is_fiscal THEN 0.16 ELSE NULL END,  -- tax_rate es fracción numeric(5,4); no-fiscal => NULL
  true,
  now() - (floor(random()*180) || ' days')::interval
FROM (VALUES
  ('Labial Mate Rojo Pasión','LAB-001', true,  6.50, 'Labiales'),
  ('Labial Líquido Nude','LAB-002',      false, 7.20, 'Labiales'),
  ('Lip Gloss Brillo Cristal','LAB-003', false, 5.80, 'Labiales'),
  ('Labial Larga Duración Vino','LAB-004',true, 8.00, 'Labiales'),
  ('Bálsamo Labial Coco','LAB-005',      false, 3.50, 'Labiales'),
  ('Base Líquida HD Natural','ROS-001',  true, 12.90, 'Rostro'),
  ('Polvo Compacto Traslúcido','ROS-002',true,  9.40, 'Rostro'),
  ('Corrector Alta Cobertura','ROS-003', false, 6.75, 'Rostro'),
  ('Rubor en Polvo Durazno','ROS-004',   false, 5.90, 'Rostro'),
  ('Iluminador Champagne','ROS-005',     true,  8.60, 'Rostro'),
  ('Primer Matificante','ROS-006',       false, 7.30, 'Rostro'),
  ('Paleta de Sombras Sunset','OJO-001', true, 14.50, 'Ojos'),
  ('Máscara de Pestañas Volumen','OJO-002',false,6.20,'Ojos'),
  ('Delineador Líquido Negro','OJO-003', false, 4.80, 'Ojos'),
  ('Lápiz de Cejas Marrón','OJO-004',    false, 3.90, 'Ojos'),
  ('Sombra Individual Glitter','OJO-005',false, 4.20, 'Ojos'),
  ('Sérum Vitamina C 30ml','FAC-001',    true, 15.90, 'Cuidado Facial'),
  ('Crema Ácido Hialurónico','FAC-002',  true, 13.40, 'Cuidado Facial'),
  ('Limpiador Facial Espuma','FAC-003',  false, 8.20, 'Cuidado Facial'),
  ('Protector Solar FPS 50','FAC-004',   true, 11.75, 'Cuidado Facial'),
  ('Mascarilla de Arcilla','FAC-005',    false, 6.60, 'Cuidado Facial'),
  ('Tónico Facial Rosas','FAC-006',      false, 7.10, 'Cuidado Facial'),
  ('Contorno de Ojos','FAC-007',         true, 10.30, 'Cuidado Facial'),
  ('Crema Corporal Karité','COR-001',    false, 9.50, 'Cuidado Corporal'),
  ('Exfoliante Corporal Café','COR-002', false, 8.80, 'Cuidado Corporal'),
  ('Aceite Corporal Almendras','COR-003',false, 7.40, 'Cuidado Corporal'),
  ('Gel de Ducha Lavanda','COR-004',     false, 5.30, 'Cuidado Corporal'),
  ('Shampoo Anticaída 400ml','CAB-001',  true, 10.90, 'Shampoo'),
  ('Acondicionador Keratina','CAB-002',  false, 9.20, 'Shampoo'),
  ('Shampoo Matizador Violeta','CAB-003',false,11.50, 'Shampoo'),
  ('Mascarilla Capilar Reparadora','TRA-001',true,12.30,'Tratamientos'),
  ('Sérum Capilar Puntas','TRA-002',     false, 8.90, 'Tratamientos'),
  ('Ampolla de Colágeno','TRA-003',      false, 3.20, 'Tratamientos'),
  ('Perfume Floral 50ml','FRA-001',      true, 28.90, 'Fragancias'),
  ('Perfume Amaderado 100ml','FRA-002',  true, 34.50, 'Fragancias'),
  ('Body Splash Frutal','FRA-003',       false, 9.80, 'Fragancias'),
  ('Perfume Cítrico Unisex','FRA-004',   true, 26.40, 'Fragancias'),
  ('Esmalte Rojo Clásico','UNA-001',     false, 3.40, 'Uñas'),
  ('Esmalte Gel Nude','UNA-002',         false, 4.60, 'Uñas'),
  ('Top Coat Brillo','UNA-003',          false, 3.80, 'Uñas'),
  ('Kit Manicure 6 piezas','UNA-004',    true, 12.90, 'Uñas'),
  ('Removedor de Esmalte','UNA-005',     false, 2.90, 'Uñas'),
  ('Set de Brochas 12 piezas','ACC-001', true, 18.50, 'Accesorios'),
  ('Esponja de Maquillaje','ACC-002',    false, 2.50, 'Accesorios'),
  ('Espejo con Luz LED','ACC-003',       true, 22.00, 'Accesorios')
) d(name, sku, is_fiscal, price, cat);

-- ============================== STOCK ==============================
-- Cantidades realistas; ~6 productos quedan en bajo stock (< 10) para el flujo de alertas.
INSERT INTO product_stock (product_id, tenant_id, quantity_on_hand, last_updated_at)
SELECT p.id, p.tenant_id,
       CASE WHEN row_number() OVER (ORDER BY p.sku) % 8 = 0
            THEN floor(random()*8)::numeric            -- bajo stock
            ELSE (15 + floor(random()*220))::numeric end,
       now()
FROM products p WHERE p.tenant_id = (SELECT tenant_id FROM _ctx);

-- ============================== PROVEEDORES (10) ==============================
INSERT INTO suppliers (id, tenant_id, name, rif, contact_name, email, phone, address, active, created_at)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), s.name, s.rif, s.contact, s.email, s.phone, s.addr,
       (row_number() OVER (ORDER BY s.name)) <> 10,  -- uno inactivo
       now() - (floor(random()*220) || ' days')::interval
FROM (VALUES
  ('Distribuidora Belleza Total C.A.','J-30512345-6','María González','ventas@bellezatotal.com','0212-5551234','Av. Libertador, Caracas'),
  ('Cosméticos del Caribe S.A.','J-31088776-2','Carlos Rodríguez','compras@cosmeticoscaribe.com','0261-4478899','Zona Industrial, Maracaibo'),
  ('Importadora Glam Venezuela','J-29944556-1','Ana Herrera','info@glamvzla.com','0212-9987766','Chacao, Caracas'),
  ('Beauty Supply Express','J-40233118-9','Luis Martínez','pedidos@beautysupply.com','0241-6634521','Valencia, Carabobo'),
  ('Fragancias Premium C.A.','J-30776654-3','Gabriela Díaz','ventas@fragapremium.com','0212-2213344','Las Mercedes, Caracas'),
  ('DermoStock Nacional','J-31200987-5','Pedro Sánchez','contacto@dermostock.com','0212-3345678','La Candelaria, Caracas'),
  ('Uñas y Más Distribuciones','J-29877665-4','Rosa Pérez','ventas@unasymas.com','0212-7788990','Petare, Caracas'),
  ('Capilar Pro Importaciones','J-40556677-8','Jorge Ramírez','info@capilarpro.com','0212-5566778','Los Palos Grandes, Caracas'),
  ('MakeUp Wholesale VE','J-30998877-0','Daniela Torres','mayor@makeupwholesale.com','0212-1122334','El Rosal, Caracas'),
  ('Esencia Natural C.A.','J-31445566-7','Andrés Blanco','ventas@esencianatural.com','0212-4455667','Baruta, Caracas')
) s(name, rif, contact, email, phone, addr);

-- ============================== ÓRDENES DE COMPRA (15) ==============================
CREATE TEMP TABLE _po ON COMMIT DROP AS
SELECT gen_random_uuid() AS id, g AS n,
       (ARRAY['draft','ordered','ordered','received','received','received','cancelled'])[1 + floor(random()*7)]::po_status AS status,
       sup.id AS supplier_id,
       now() - (floor(random()*90) || ' days')::interval AS created_at
FROM generate_series(1,15) g
CROSS JOIN LATERAL (
  SELECT id FROM suppliers WHERE tenant_id=(SELECT tenant_id FROM _ctx) AND active ORDER BY random() LIMIT 1
) sup;

INSERT INTO purchase_orders (id, tenant_id, supplier_id, status, notes, total, ordered_at, received_at, created_by, created_at, updated_at)
SELECT po.id, (SELECT tenant_id FROM _ctx), po.supplier_id, po.status,
       CASE WHEN random() < 0.3 THEN 'Reposición mensual de inventario' ELSE NULL END,
       0,
       CASE WHEN po.status IN ('ordered','received') THEN po.created_at + interval '1 day' END,
       CASE WHEN po.status = 'received' THEN po.created_at + interval '5 days' END,
       (SELECT owner_id FROM _ctx), po.created_at, po.created_at
FROM _po po;

INSERT INTO purchase_order_lines (id, po_id, product_id, description, quantity_ordered, unit_cost, subtotal, sort_order)
SELECT gen_random_uuid(), po.id, p.id, p.name, p.qty,
       round(COALESCE(p.internal_price, p.fiscal_price) * 0.6, 2),
       round(p.qty * COALESCE(p.internal_price, p.fiscal_price) * 0.6, 2),
       p.rn
FROM _po po
CROSS JOIN LATERAL (
  SELECT pr.id, pr.name, pr.internal_price, pr.fiscal_price,
         (10 + floor(random()*40))::numeric AS qty,
         row_number() OVER (ORDER BY random()) AS rn
  FROM products pr WHERE pr.tenant_id=(SELECT tenant_id FROM _ctx)
  ORDER BY random() LIMIT (2 + floor(random()*4))::int
) p;

UPDATE purchase_orders po SET total = COALESCE((SELECT sum(subtotal) FROM purchase_order_lines WHERE po_id=po.id),0)
WHERE po.tenant_id = (SELECT tenant_id FROM _ctx);

-- Factura del proveedor para las OC recibidas (Fase 1: declarar la compra).
UPDATE purchase_orders po SET
  supplier_invoice_number     = 'FAC-' || lpad((10000 + floor(random()*89999))::text, 5, '0'),
  supplier_invoice_date       = po.received_at::date,
  supplier_invoice_tax_base   = round(po.total, 2),
  supplier_invoice_tax_amount = round(po.total * 0.16, 2)
WHERE po.tenant_id = (SELECT tenant_id FROM _ctx) AND po.status = 'received';

-- ============================== MOVIMIENTOS DE INVENTARIO ==============================
-- Apertura (uno por producto)
INSERT INTO inventory_movements (id, tenant_id, product_id, type, quantity, unit_cost, reference_type, reference_id, notes, created_by, created_at)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), p.id, 'entry',
       (SELECT quantity_on_hand FROM product_stock WHERE product_id=p.id),
       round(COALESCE(p.internal_price,p.fiscal_price)*0.6,2), 'opening', NULL, 'Inventario inicial',
       (SELECT owner_id FROM _ctx), p.created_at
FROM products p WHERE p.tenant_id=(SELECT tenant_id FROM _ctx);

-- Entradas por OC recibidas
INSERT INTO inventory_movements (id, tenant_id, product_id, type, quantity, unit_cost, reference_type, reference_id, notes, created_by, created_at)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), pol.product_id, 'entry', pol.quantity_ordered,
       pol.unit_cost, 'purchase_order', po.id, 'Recepción de orden de compra',
       (SELECT owner_id FROM _ctx), po.received_at
FROM purchase_orders po JOIN purchase_order_lines pol ON pol.po_id=po.id
WHERE po.tenant_id=(SELECT tenant_id FROM _ctx) AND po.status='received';

-- Ajustes manuales (12)
INSERT INTO inventory_movements (id, tenant_id, product_id, type, quantity, unit_cost, reference_type, reference_id, notes, created_by, created_at)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), prod.id,
       'adjustment', (1 + floor(random()*6))::numeric * (CASE WHEN random()<0.5 THEN 1 ELSE -1 END),
       NULL, 'manual_adjustment', NULL,
       (ARRAY['Merma por vencimiento','Corrección de conteo','Producto dañado','Reconteo físico'])[1+floor(random()*4)],
       (SELECT owner_id FROM _ctx), now() - (floor(random()*60)||' days')::interval
FROM generate_series(1,12) g
CROSS JOIN LATERAL (SELECT id FROM products WHERE tenant_id=(SELECT tenant_id FROM _ctx) ORDER BY random() LIMIT 1) prod;

-- ============================== ÓRDENES DE VENTA / POS (30) ==============================
CREATE TEMP TABLE _so ON COMMIT DROP AS
SELECT gen_random_uuid() AS id, g AS n,
       (ARRAY['draft','confirmed','confirmed','invoiced','invoiced','invoiced','cancelled'])[1 + floor(random()*7)]::sale_order_status AS status,
       (ARRAY['anonymous','anonymous','cedula','cedula','rif'])[1 + floor(random()*5)]::customer_id_type AS idt,
       now() - (floor(random()*75) || ' days')::interval AS created_at
FROM generate_series(1,30) g;

INSERT INTO sales_orders (id, tenant_id, status, customer_name, customer_id_type, customer_id_number, notes, total, confirmed_at, invoiced_at, invoice_id, created_by, created_at, updated_at)
SELECT so.id, (SELECT tenant_id FROM _ctx), so.status,
       CASE WHEN so.idt='anonymous' THEN NULL ELSE (ARRAY['Yorman Pérez','Keila Rondón','Génesis Marcano','Wilmer Suárez','Dayana Colmenares','Franklin Ríos'])[1+floor(random()*6)] END,
       so.idt,
       CASE so.idt WHEN 'cedula' THEN 'V-'||(10000000+floor(random()*20000000))::text
                   WHEN 'rif' THEN 'J-'||(300000000+floor(random()*90000000))::text||'-'||floor(random()*9)::text
                   ELSE NULL END,
       NULL, 0,
       CASE WHEN so.status IN ('confirmed','invoiced') THEN so.created_at + interval '2 hours' END,
       CASE WHEN so.status='invoiced' THEN so.created_at + interval '3 hours' END,
       NULL,
       (SELECT owner_id FROM _ctx), so.created_at, so.created_at
FROM _so so;

INSERT INTO sales_order_lines (id, order_id, product_id, description, quantity, unit_price, subtotal, sort_order)
SELECT gen_random_uuid(), so.id, p.id, p.name, p.qty, p.price, round(p.qty*p.price,2), p.rn
FROM _so so
CROSS JOIN LATERAL (
  SELECT p.id, p.name, COALESCE(p.internal_price,p.fiscal_price) AS price,
         (1 + floor(random()*4))::numeric AS qty, row_number() OVER () AS rn
  FROM products p WHERE p.tenant_id=(SELECT tenant_id FROM _ctx) AND p.active
  ORDER BY random() LIMIT (1 + floor(random()*3))::int
) p;

UPDATE sales_orders so SET total = COALESCE((SELECT sum(subtotal) FROM sales_order_lines WHERE order_id=so.id),0)
WHERE so.tenant_id=(SELECT tenant_id FROM _ctx);

-- Salidas de inventario por ventas confirmadas/facturadas
INSERT INTO inventory_movements (id, tenant_id, product_id, type, quantity, unit_cost, reference_type, reference_id, notes, created_by, created_at)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), sol.product_id, 'exit', sol.quantity, NULL,
       'sale', so.id, 'Salida por venta', (SELECT owner_id FROM _ctx), so.created_at
FROM sales_orders so JOIN sales_order_lines sol ON sol.order_id=so.id
WHERE so.tenant_id=(SELECT tenant_id FROM _ctx) AND so.status IN ('confirmed','invoiced');

-- ============================== FACTURAS INTERNAS (20) ==============================
CREATE TEMP TABLE _inv ON COMMIT DROP AS
SELECT gen_random_uuid() AS id, g AS n,
       (ARRAY['draft','issued','issued','issued','cancelled'])[1 + floor(random()*5)]::invoice_status AS status,
       (ARRAY['anonymous','cedula','rif']) [1 + floor(random()*3)]::customer_id_type AS idt,
       now() - (floor(random()*70) || ' days')::interval AS created_at
FROM generate_series(1,20) g;

INSERT INTO internal_invoices (id, tenant_id, correlative, customer_name, customer_id_type, customer_id_number, status, subtotal, total, notes, issued_at, created_by, created_at, updated_at)
SELECT iv.id, (SELECT tenant_id FROM _ctx),
       CASE WHEN iv.status='issued' THEN '00-'||lpad((row_number() OVER (PARTITION BY (iv.status='issued') ORDER BY iv.created_at))::text, 8, '0') END,
       CASE WHEN iv.idt='anonymous' THEN 'Consumidor Final' ELSE (ARRAY['Yohana Silva','Rafael Mora','Nairobi Guzmán','Éider Villalba','Marielys Ochoa'])[1+floor(random()*5)] END,
       iv.idt,
       CASE iv.idt WHEN 'cedula' THEN 'V-'||(10000000+floor(random()*20000000))::text
                   WHEN 'rif' THEN 'J-'||(300000000+floor(random()*90000000))::text||'-'||floor(random()*9)::text
                   ELSE NULL END,
       iv.status, 0, 0,
       CASE WHEN random()<0.2 THEN 'Gracias por su compra' ELSE NULL END,
       CASE WHEN iv.status='issued' THEN iv.created_at + interval '1 hour' END,
       (SELECT owner_id FROM _ctx), iv.created_at, iv.created_at
FROM _inv iv;

INSERT INTO internal_invoice_lines (id, invoice_id, product_id, description, quantity, unit_price, subtotal, sort_order)
SELECT gen_random_uuid(), iv.id, p.id, p.name, p.qty, p.price, round(p.qty*p.price,2), p.rn
FROM _inv iv
CROSS JOIN LATERAL (
  SELECT p.id, p.name, COALESCE(p.internal_price,p.fiscal_price) AS price,
         (1 + floor(random()*3))::numeric AS qty, row_number() OVER () AS rn
  FROM products p WHERE p.tenant_id=(SELECT tenant_id FROM _ctx) AND p.active
  ORDER BY random() LIMIT (1 + floor(random()*3))::int
) p;

UPDATE internal_invoices iv
SET subtotal = COALESCE((SELECT sum(subtotal) FROM internal_invoice_lines WHERE invoice_id=iv.id),0),
    total    = COALESCE((SELECT sum(subtotal) FROM internal_invoice_lines WHERE invoice_id=iv.id),0)
WHERE iv.tenant_id=(SELECT tenant_id FROM _ctx);

-- ============================== FACTURAS FISCALES (12) ==============================
CREATE TEMP TABLE _fi ON COMMIT DROP AS
SELECT gen_random_uuid() AS id, g AS n,
       (ARRAY['draft','issued','issued','issued','failed','cancelled'])[1 + floor(random()*6)]::fiscal_invoice_status AS status,
       (ARRAY['cedula','rif','rif']) [1 + floor(random()*3)]::customer_id_type AS idt,
       now() - (floor(random()*65) || ' days')::interval AS created_at
FROM generate_series(1,12) g;

INSERT INTO fiscal_invoices (id, tenant_id, fiscal_number, machine_serial, report_z_number, customer_name, customer_id_type, customer_id_number, subtotal_base, tax_amount, total, status, fail_reason, notes, issued_at, created_by, created_at, updated_at)
SELECT fi.id, (SELECT tenant_id FROM _ctx),
       CASE WHEN fi.status='issued' THEN '00-'||lpad((row_number() OVER (PARTITION BY (fi.status='issued') ORDER BY fi.created_at))::text,8,'0') END,
       CASE WHEN fi.status='issued' THEN 'Z1B4000'||(1000+floor(random()*8999))::text END,
       CASE WHEN fi.status='issued' THEN (100+floor(random()*400))::int END,
       (ARRAY['Comercial La Económica','Farmacia San José','Boutique Elegance','Inversiones GHT','Distribuidora Mavesa'])[1+floor(random()*5)],
       fi.idt,
       CASE fi.idt WHEN 'cedula' THEN 'V-'||(10000000+floor(random()*20000000))::text
                   ELSE 'J-'||(300000000+floor(random()*90000000))::text||'-'||floor(random()*9)::text END,
       0, 0, 0, fi.status,
       CASE WHEN fi.status='failed' THEN 'Impresora fiscal sin comunicación (timeout SENIAT)' END,
       NULL,
       CASE WHEN fi.status='issued' THEN fi.created_at + interval '30 minutes' END,
       (SELECT owner_id FROM _ctx), fi.created_at, fi.created_at
FROM _fi fi;

-- Solo productos fiscales en facturas fiscales
INSERT INTO fiscal_invoice_lines (id, invoice_id, product_id, description, quantity, unit_price, tax_rate, tax_amount, subtotal, sort_order)
SELECT gen_random_uuid(), fi.id, p.id, p.name, p.qty, p.price, 0.16,
       round(p.qty*p.price*0.16,2), round(p.qty*p.price,2), p.rn
FROM _fi fi
CROSS JOIN LATERAL (
  SELECT p.id, p.name, COALESCE(p.fiscal_price,p.internal_price) AS price,
         (1 + floor(random()*3))::numeric AS qty, row_number() OVER () AS rn
  FROM products p WHERE p.tenant_id=(SELECT tenant_id FROM _ctx) AND p.is_fiscal AND p.active
  ORDER BY random() LIMIT (1 + floor(random()*3))::int
) p;

UPDATE fiscal_invoices fi SET
  subtotal_base = COALESCE((SELECT sum(subtotal)   FROM fiscal_invoice_lines WHERE invoice_id=fi.id),0),
  tax_amount    = COALESCE((SELECT sum(tax_amount) FROM fiscal_invoice_lines WHERE invoice_id=fi.id),0),
  total         = COALESCE((SELECT sum(subtotal+tax_amount) FROM fiscal_invoice_lines WHERE invoice_id=fi.id),0)
WHERE fi.tenant_id=(SELECT tenant_id FROM _ctx);

-- ============================== MOTORIZADOS DEMO (3) ==============================
-- user_id de riders.riders apunta a tenant_users(id), no a auth.users — no hace
-- falta una cuenta real de Supabase Auth para que el rider aparezca y se le
-- puedan asignar pedidos en el panel. ponytail: estos riders demo no pueden
-- loguearse de verdad (no tienen auth.users detrás); si hace falta probar el
-- login real del rider, invitarlo desde /settings como cualquier usuario.
INSERT INTO tenant_users (id, tenant_id, supabase_uid, email, role, active)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), gen_random_uuid(), r.email, 'rider', true
FROM (VALUES
  ('motorizado.demo1@bond-seed.local'),
  ('motorizado.demo2@bond-seed.local'),
  ('motorizado.demo3@bond-seed.local')
) r(email);

INSERT INTO riders (id, tenant_id, user_id, name, phone, rate_per_delivery, status, max_concurrent_deliveries, active, created_at)
SELECT gen_random_uuid(), (SELECT tenant_id FROM _ctx), tu.id, d.name, d.phone, d.rate, d.status, d.max_concurrent, true,
       now() - (floor(random()*30) || ' days')::interval
FROM (VALUES
  ('motorizado.demo1@bond-seed.local', 'Carlos Pérez',   '0414-1234567', 2.50, 'available', 3),
  ('motorizado.demo2@bond-seed.local', 'Rafael Soto',    '0424-2345678', 2.50, 'available', 2),
  ('motorizado.demo3@bond-seed.local', 'Daniela Torres', '0412-3456789', 3.00, 'unavailable', 2)
) d(email, name, phone, rate, status, max_concurrent)
JOIN tenant_users tu ON tu.tenant_id = (SELECT tenant_id FROM _ctx) AND tu.email = d.email;

-- ============================== PEDIDOS TIENDA / B2C (30) ==============================
CREATE TEMP TABLE _shop ON COMMIT DROP AS
SELECT gen_random_uuid() AS id, g AS n,
       (ARRAY['pending','pending','paid','paid','fulfilled','delivered','delivered','cancelled'])[1 + floor(random()*8)]::shop_order_status AS status,
       now() - (floor(random()*60) || ' days')::interval AS created_at
FROM generate_series(1,30) g;

INSERT INTO shop_orders (id, tenant_id, customer_name, customer_email, customer_phone, shipping_address, shipping_city, shipping_notes,
                         delivery_lat, delivery_lng,
                         subtotal, shipping_cost, total, status, notes, access_token, paid_at, fulfilled_at, delivered_at, cancelled_at,
                         payment_method_id, payment_reference, created_at, updated_at)
SELECT sh.id, (SELECT tenant_id FROM _ctx),
       (ARRAY['Valentina Rojas','Sebastián Uzcátegui','Camila Fernández','Alejandro Graterol','Isabella Nava','Mateo Bracho','Sofía Villegas','Diego Paredes'])[1+floor(random()*8)],
       'cliente'||sh.n||'@gmail.com',
       '0'||(414+floor(random()*20))::text||'-'||(1000000+floor(random()*8999999))::text,
       'Calle '||(1+floor(random()*80))::text||', Residencias Las Acacias, piso '||(1+floor(random()*10))::text,
       (ARRAY['Caracas','Maracaibo','Valencia','Barquisimeto','Maracay','Maturín'])[1+floor(random()*6)],
       CASE WHEN random()<0.3 THEN 'Entregar en horario de tarde' ELSE NULL END,
       -- Jitter alrededor del centro de Caracas — no corresponde a la shipping_city real, es demo.
       10.4806 + (random()-0.5)*0.15, -66.9036 + (random()-0.5)*0.15,
       0, (ARRAY[0,3,5,4])[1+floor(random()*4)], 0, sh.status,
       NULL, replace(gen_random_uuid()::text,'-',''),
       CASE WHEN sh.status IN ('paid','fulfilled','delivered') THEN sh.created_at + interval '4 hours' END,
       CASE WHEN sh.status IN ('fulfilled','delivered') THEN sh.created_at + interval '1 day' END,
       CASE WHEN sh.status='delivered' THEN sh.created_at + interval '3 days' END,
       CASE WHEN sh.status='cancelled' THEN sh.created_at + interval '6 hours' END,
       CASE WHEN sh.status IN ('paid','fulfilled','delivered')
            THEN (CASE WHEN random()<0.5 THEN (SELECT pm_pagomovil FROM _ctx) ELSE (SELECT pm_zelle FROM _ctx) END) END,
       CASE WHEN sh.status IN ('paid','fulfilled','delivered') THEN 'REF'||(10000+floor(random()*89999))::text END,
       sh.created_at, sh.created_at
FROM _shop sh;

INSERT INTO shop_order_lines (id, order_id, product_id, name, unit_price, quantity, subtotal, is_fiscal, sort_order)
SELECT gen_random_uuid(), sh.id, p.id, p.name, p.price, p.qty::int, round(p.qty*p.price,2), p.is_fiscal, p.rn
FROM _shop sh
CROSS JOIN LATERAL (
  SELECT p.id, p.name, COALESCE(p.internal_price,p.fiscal_price) AS price, p.is_fiscal,
         (1 + floor(random()*3))::numeric AS qty, row_number() OVER () AS rn
  FROM products p WHERE p.tenant_id=(SELECT tenant_id FROM _ctx) AND p.active
  ORDER BY random() LIMIT (1 + floor(random()*4))::int
) p;

UPDATE shop_orders sh SET
  subtotal = COALESCE((SELECT sum(subtotal) FROM shop_order_lines WHERE order_id=sh.id),0),
  total    = COALESCE((SELECT sum(subtotal) FROM shop_order_lines WHERE order_id=sh.id),0) + sh.shipping_cost
WHERE sh.tenant_id=(SELECT tenant_id FROM _ctx);

-- Asignar la mitad de los pedidos fulfilled/delivered a un motorizado demo,
-- con un par marcados urgentes y los delivered ya liquidados al rider.
WITH eligible AS (
  SELECT id, status, row_number() OVER (ORDER BY created_at) AS rn
  FROM shop_orders
  WHERE tenant_id = (SELECT tenant_id FROM _ctx) AND status IN ('fulfilled','delivered')
), picked AS (
  SELECT id, status FROM eligible WHERE rn % 2 = 0
)
UPDATE shop_orders sh SET
  rider_id           = (SELECT id FROM riders WHERE tenant_id=(SELECT tenant_id FROM _ctx) ORDER BY random() LIMIT 1),
  rider_fee           = (SELECT rate_per_delivery FROM riders WHERE tenant_id=(SELECT tenant_id FROM _ctx) ORDER BY random() LIMIT 1),
  rider_assigned_at   = sh.created_at + interval '30 minutes',
  rider_delivered_at  = CASE WHEN picked.status = 'delivered' THEN sh.created_at + interval '2 hours' END,
  rider_paid_at       = CASE WHEN picked.status = 'delivered' AND random() < 0.6 THEN sh.created_at + interval '1 day' END,
  is_urgent           = picked.id IN (SELECT id FROM picked ORDER BY id LIMIT 2),
  urgent_fee          = CASE WHEN picked.id IN (SELECT id FROM picked ORDER BY id LIMIT 2)
                              THEN (SELECT urgent_delivery_fee FROM tenants WHERE id=(SELECT tenant_id FROM _ctx)) END
FROM picked
WHERE sh.id = picked.id;

-- ============================== SECUENCIA DE CORRELATIVOS ==============================
INSERT INTO invoice_correlative_seq (tenant_id, year, last_seq)
SELECT (SELECT tenant_id FROM _ctx), EXTRACT(YEAR FROM now())::int,
       (SELECT count(*) FROM internal_invoices WHERE tenant_id=(SELECT tenant_id FROM _ctx) AND status='issued')
     + (SELECT count(*) FROM fiscal_invoices   WHERE tenant_id=(SELECT tenant_id FROM _ctx) AND status='issued');

SET session_replication_role = DEFAULT;
COMMIT;
