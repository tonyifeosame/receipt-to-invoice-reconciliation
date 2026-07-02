-- Receipt-to-Invoice Reconciliation System
-- Sample seed data for testing

-- Insert sample invoices
INSERT INTO invoices (invoice_number, customer_name, amount, status, due_date) VALUES
('INV-001', 'Acme Corporation', 1500.00, 'PENDING', '2026-07-15'),
('INV-002', 'Tech Solutions Ltd', 2750.50, 'PENDING', '2026-07-20'),
('INV-003', 'Global Industries', 5000.00, 'PENDING', '2026-07-25'),
('INV-004', 'StartUp Ventures', 875.25, 'PENDING', '2026-07-30'),
('INV-005', 'Enterprise Systems', 3200.00, 'PENDING', '2026-08-05');

-- Insert sample audit logs
INSERT INTO audit_logs (action, description, user_name) VALUES
('SYSTEM_INIT', 'Database initialized with seed data', 'system'),
('INVOICE_CREATED', 'Sample invoices created for testing', 'system');
