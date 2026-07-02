# Receipt-to-Invoice Reconciliation System

A system that automatically reconciles customer payment receipts with outstanding invoices using Optical Character Recognition (OCR). The application extracts payment information from scanned receipts, matches it against invoice records, updates payment status, and maintains a complete audit trail for financial accountability.

## Technologies

- **C++17/20** - OCR Engine
- **OpenCV** - Image preprocessing
- **Tesseract OCR** - Text extraction
- **Go** - REST API Backend
- **PostgreSQL** - Database
- **libpqxx** - PostgreSQL C++ client
- **CMake** - Build system
- **Docker** - Containerization

## Architecture

```
Receipt Image
       │
       ▼
C++ OCR Service (OpenCV + Tesseract OCR)
       │
Extract Receipt Data
       │
       ▼
Go REST API
       │
Business Logic Layer
       │
       ▼
PostgreSQL Database
       │                   │
       ▼                   ▼
Invoice Records      Audit Logs
```

## Project Structure

```
receipt-reconciliation/
├── ocr-engine/              # C++ OCR processing engine
│   ├── include/            # Header files
│   │   ├── image_processor.h
│   │   ├── ocr_engine.h
│   │   ├── receipt_parser.h
│   │   ├── database.h
│   │   └── logger.h
│   ├── src/                # Implementation files
│   │   ├── image_processor.cpp
│   │   ├── ocr_engine.cpp
│   │   ├── receipt_parser.cpp
│   │   ├── database.cpp
│   │   ├── logger.cpp
│   │   └── main.cpp
│   ├── CMakeLists.txt
│   ├── config.json
│   └── Dockerfile
├── backend/                 # Go REST API
│   ├── handlers/           # HTTP handlers
│   ├── middleware/         # HTTP middleware
│   ├── models/             # Data models
│   ├── repository/         # Database layer
│   ├── main.go
│   ├── go.mod
│   ├── .env.example
│   └── Dockerfile
├── database/               # Database schema
│   ├── migrations/
│   │   └── 001_init.sql
│   └── seed.sql
├── receipts/               # Receipt images directory
├── docs/                   # Documentation
├── docker-compose.yml
└── README.md
```

## Features

### Receipt Scanning
- Upload receipt images (JPG, PNG)
- Image enhancement before OCR
- Automatic receipt validation

### OCR Processing
- **OpenCV preprocessing:**
  - Convert to grayscale
  - Remove noise
  - Apply thresholding
  - Correct image rotation
  - Detect receipt boundaries
- **Tesseract extraction:**
  - Invoice Number
  - Amount Paid
  - Payment Date
  - Bank Reference
  - Customer Name (optional)

### Invoice Matching
- Search PostgreSQL for the invoice
- Match by invoice number
- Validate payment amount
- Detect duplicate payments
- Flag unmatched receipts

### Automatic Reconciliation
- Mark invoice as paid
- Save payment details
- Update invoice balance
- Prevent duplicate reconciliation

### Reconciliation History
- View processed receipts
- Search reconciliation records
- Export reports
- Filter by date or invoice

### Audit Logs
- Record every important action:
  - Receipt uploaded
  - OCR completed
  - Invoice matched
  - Payment confirmed
  - Manual corrections
  - User activity

## REST API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/receipts/upload` | Upload receipt image |
| POST | `/ocr/process` | Process OCR on receipt |
| POST | `/reconcile` | Reconcile payment with invoice |
| GET | `/history` | Get reconciliation history |
| GET | `/payments` | Get payment records |
| GET | `/audit-logs` | Get audit logs |
| GET | `/invoices` | Get invoice details |
| GET | `/health` | Health check |

## Database Schema

### invoices
- `id` - Primary key
- `invoice_number` - Unique invoice identifier
- `customer_name` - Customer name
- `amount` - Invoice amount
- `status` - PENDING, PAID, OVERDUE, CANCELLED
- `due_date` - Due date
- `created_at` - Creation timestamp

### payments
- `id` - Primary key
- `invoice_id` - Foreign key to invoices
- `receipt_file` - Receipt file path
- `payment_amount` - Payment amount
- `payment_date` - Payment date
- `reference` - Bank reference
- `created_at` - Creation timestamp

### reconciliation_history
- `id` - Primary key
- `invoice_number` - Invoice number
- `receipt_name` - Receipt file name
- `status` - MATCHED, UNMATCHED, PARTIAL
- `matched_at` - Match timestamp

### audit_logs
- `id` - Primary key
- `action` - Action performed
- `description` - Action description
- `user_name` - User who performed action
- `timestamp` - Action timestamp

## Prerequisites

### For Local Development

**C++ OCR Engine:**
- C++17/20 compiler (GCC, Clang, or MSVC)
- CMake 3.15+
- OpenCV 4.x
- Tesseract OCR
- Leptonica
- libpqxx
- PostgreSQL client libraries
- nlohmann/json library

**Go Backend:**
- Go 1.21+
- PostgreSQL driver

**Database:**
- PostgreSQL 15+

### For Docker Deployment
- Docker 20.10+
- Docker Compose 2.0+

## Installation

### Option 1: Docker (Recommended)

1. Clone the repository:
```bash
git clone <repository-url>
cd receipt-reconciliation
```

2. Build and start all services:
```bash
docker compose up --build -d
```

3. The services will be available at:
- Finance dashboard: http://localhost:8080
- Go Backend API: http://localhost:8080
- PostgreSQL: localhost:5432

4. Check service health:
```bash
docker compose ps
docker compose logs -f backend
```

5. Stop the stack:
```bash
docker compose down
```

For a full walkthrough, see [docs/deployment.md](docs/deployment.md).

### Option 2: Local Development

#### Database Setup

1. Install PostgreSQL 15+

2. Create the database:
```bash
createdb receipt_reconciliation
```

3. Run migrations:
```bash
psql -d receipt_reconciliation -f database/migrations/001_init.sql
```

4. Load seed data (optional):
```bash
psql -d receipt_reconciliation -f database/seed.sql
```

#### Go Backend Setup

1. Navigate to the backend directory:
```bash
cd backend
```

2. Install dependencies:
```bash
go mod download
```

3. Create environment file:
```bash
cp .env.example .env
```

4. Edit `.env` with your database credentials:
```
DB_HOST=localhost
DB_PORT=5432
DB_NAME=receipt_reconciliation
DB_USER=postgres
DB_PASSWORD=your_password
PORT=8080
```

5. Run the server:
```bash
go run main.go
```

#### C++ OCR Engine Setup

1. Install dependencies:

**Ubuntu/Debian:**
```bash
sudo apt-get update
sudo apt-get install -y build-essential cmake pkg-config
sudo apt-get install -y libopencv-dev libtesseract-dev libleptonica-dev
sudo apt-get install -y libpqxx-dev postgresql-client
sudo apt-get install -y tesseract-ocr-eng nlohmann-json3-dev
```

**macOS (Homebrew):**
```bash
brew install opencv tesseract leptonica pqxx nlohmann-json
```

2. Navigate to the OCR engine directory:
```bash
cd ocr-engine
```

3. Build the project:
```bash
mkdir build
cd build
cmake ..
make
```

4. Run the OCR engine:
```bash
./ocr_engine
```

## Usage

### Processing a Single Receipt

Using the C++ OCR engine:
```bash
cd ocr-engine/build
./ocr_engine config.json ../receipts/receipt.jpg
```

### Processing All Receipts in Directory

```bash
cd ocr-engine/build
./ocr_engine config.json
```

### Using the REST API

**Upload a receipt:**
```bash
curl -X POST http://localhost:8080/receipts/upload \
  -H "Content-Type: application/json" \
  -d '{"file_path": "/path/to/receipt.jpg"}'
```

**Process OCR:**
```bash
curl -X POST http://localhost:8080/ocr/process \
  -H "Content-Type: application/json" \
  -d '{"receipt_file": "/path/to/receipt.jpg"}'
```

**Reconcile payment:**
```bash
curl -X POST http://localhost:8080/reconcile \
  -H "Content-Type: application/json" \
  -d '{
    "invoice_number": "INV-001",
    "amount_paid": 1500.00,
    "payment_date": "2026-06-29",
    "reference": "REF12345",
    "receipt_file": "/path/to/receipt.jpg"
  }'
```

**Get reconciliation history:**
```bash
curl http://localhost:8080/history
```

**Get audit logs:**
```bash
curl http://localhost:8080/audit-logs
```

**Get invoice details:**
```bash
curl "http://localhost:8080/invoices?invoice_number=INV-001"
```

## Configuration

### OCR Engine Configuration

Edit `ocr-engine/config.json`:

```json
{
  "database": {
    "host": "localhost",
    "port": 5432,
    "dbname": "receipt_reconciliation",
    "user": "postgres",
    "password": "postgres"
  },
  "tesseract": {
    "data_path": "",
    "language": "eng"
  },
  "receipts_dir": "../receipts",
  "log_file": "ocr_engine.log"
}
```

### Go Backend Configuration

Edit `backend/.env`:

```
DB_HOST=localhost
DB_PORT=5432
DB_NAME=receipt_reconciliation
DB_USER=postgres
DB_PASSWORD=postgres
PORT=8080
```

## Development Phases

### Phase 1 ✅
- Set up CMake project
- Integrate OpenCV
- Integrate Tesseract OCR

### Phase 2 ✅
- Receipt image preprocessing
- OCR extraction
- Text parsing

### Phase 3 ✅
- PostgreSQL integration
- Invoice lookup
- Payment matching

### Phase 4 ✅
- Go REST API
- Upload endpoint
- Reconciliation endpoint

### Phase 5 ✅
- Audit logging
- History
- Error handling
- Unit tests (to be added)

## Testing

### Manual Testing

1. Place receipt images in the `receipts/` directory
2. Run the OCR engine to process them
3. Check the database for updated invoice statuses
4. Verify audit logs are created

### API Testing

Use tools like Postman or curl to test the REST API endpoints.

## Troubleshooting

### Tesseract OCR Issues

If Tesseract fails to initialize:
- Ensure Tesseract is installed correctly
- Check the `data_path` in config.json
- Verify language files are installed

### Database Connection Issues

If the OCR engine cannot connect to the database:
- Verify PostgreSQL is running
- Check connection parameters in config.json
- Ensure the database exists

### OpenCV Issues

If OpenCV fails to load images:
- Verify OpenCV is installed correctly
- Check image file paths
- Ensure image formats are supported (JPG, PNG)

## License

This project is provided as-is for educational and commercial use.

## Contributing

Contributions are welcome! Please follow these steps:
1. Fork the repository
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

## Support

For issues and questions, please open an issue on the repository.

## Production readiness documentation

- [docs/deployment.md](docs/deployment.md)
- [docs/operations.md](docs/operations.md)
- [docs/security.md](docs/security.md)
- [docs/backup-recovery.md](docs/backup-recovery.md)
- [docs/user-guide.md](docs/user-guide.md)

## Monitoring stack

The deployment stack now includes:
- Prometheus at http://localhost:9090
- Grafana at http://localhost:3000
- Alertmanager at http://localhost:9093

Use the provided configuration in [monitoring/prometheus.yml](monitoring/prometheus.yml) and [monitoring/alertmanager.yml](monitoring/alertmanager.yml).
