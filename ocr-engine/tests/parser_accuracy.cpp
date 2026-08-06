// Field level accuracy harness for the receipt parser.
//
// It runs the parser over the labelled corpus and reports, per field, how many
// receipts were extracted exactly right. Building the same harness against an
// older parser gives a like-for-like before/after comparison.
//
//   ./parser_accuracy            summary table
//   ./parser_accuracy -v         also print every mismatch

#include "receipt_corpus.h"
#include "receipt_parser.h"
#include "logger.h"

#include <cmath>
#include <cstring>
#include <iomanip>
#include <iostream>
#include <string>
#include <vector>

namespace {

struct FieldStats {
    std::string name;
    int correct = 0;
    int total = 0;

    double percentage() const {
        return total == 0 ? 0.0 : (100.0 * correct) / total;
    }
};

bool amountsMatch(double expected, double actual) {
    return std::fabs(expected - actual) < 0.005;
}

std::string formatAmount(double value) {
    std::ostringstream oss;
    oss << std::fixed << std::setprecision(2) << value;
    return oss.str();
}

void record(FieldStats& stats, bool correct, bool verbose,
            const std::string& sample, const std::string& expected, const std::string& actual) {
    stats.total++;
    if (correct) {
        stats.correct++;
    } else if (verbose) {
        std::cout << "    [" << sample << "] " << stats.name
                  << ": expected \"" << expected << "\" got \"" << actual << "\"\n";
    }
}

} // namespace

int main(int argc, char* argv[]) {
    bool verbose = false;
    for (int i = 1; i < argc; i++) {
        if (std::strcmp(argv[i], "-v") == 0 || std::strcmp(argv[i], "--verbose") == 0) {
            verbose = true;
        }
    }

    // Parser logging would drown the report.
    Logger::getInstance().enableConsoleOutput(false);

    ReceiptParser parser;
    std::vector<ReceiptSample> corpus = receiptCorpus();

    FieldStats invoice{"invoice"};
    FieldStats amount{"amount"};
    FieldStats date{"date"};
    FieldStats reference{"reference"};
    FieldStats customer{"customer"};

    int perfectReceipts = 0;

    for (const auto& sample : corpus) {
        ReceiptData parsed = parser.parseReceipt(sample.text, 90.0);

        bool invoiceOk = parsed.invoiceNumber == sample.invoice;
        bool amountOk = amountsMatch(sample.amount, parsed.amountPaid);
        bool dateOk = parsed.paymentDate == sample.date;
        bool referenceOk = parsed.bankReference == sample.reference;
        bool customerOk = parsed.customerName == sample.customer;

        record(invoice, invoiceOk, verbose, sample.name, sample.invoice, parsed.invoiceNumber);
        record(amount, amountOk, verbose, sample.name, formatAmount(sample.amount), formatAmount(parsed.amountPaid));
        record(date, dateOk, verbose, sample.name, sample.date, parsed.paymentDate);
        record(reference, referenceOk, verbose, sample.name, sample.reference, parsed.bankReference);
        record(customer, customerOk, verbose, sample.name, sample.customer, parsed.customerName);

        if (invoiceOk && amountOk && dateOk && referenceOk && customerOk) {
            perfectReceipts++;
        }
    }

    std::vector<FieldStats> fields = {invoice, amount, date, reference, customer};

    int totalCorrect = 0;
    int totalFields = 0;
    for (const auto& field : fields) {
        totalCorrect += field.correct;
        totalFields += field.total;
    }

    std::cout << "\nReceipt parser field accuracy over " << corpus.size() << " receipts\n";
    std::cout << "-------------------------------------------------\n";
    std::cout << std::left << std::setw(12) << "field"
              << std::right << std::setw(10) << "correct"
              << std::setw(10) << "total"
              << std::setw(12) << "accuracy" << "\n";

    for (const auto& field : fields) {
        std::cout << std::left << std::setw(12) << field.name
                  << std::right << std::setw(10) << field.correct
                  << std::setw(10) << field.total
                  << std::setw(11) << std::fixed << std::setprecision(1) << field.percentage() << "%\n";
    }

    std::cout << "-------------------------------------------------\n";
    std::cout << std::left << std::setw(12) << "ALL FIELDS"
              << std::right << std::setw(10) << totalCorrect
              << std::setw(10) << totalFields
              << std::setw(11) << std::fixed << std::setprecision(1)
              << (totalFields == 0 ? 0.0 : (100.0 * totalCorrect) / totalFields) << "%\n";
    std::cout << std::left << std::setw(12) << "RECEIPTS"
              << std::right << std::setw(10) << perfectReceipts
              << std::setw(10) << corpus.size()
              << std::setw(11) << std::fixed << std::setprecision(1)
              << (corpus.empty() ? 0.0 : (100.0 * perfectReceipts) / corpus.size()) << "%\n";
    std::cout << "(RECEIPTS counts documents where all five fields are exactly right)\n\n";

    return 0;
}
