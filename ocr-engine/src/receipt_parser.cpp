#include "receipt_parser.h"
#include "logger.h"
#include <sstream>
#include <iomanip>
#include <algorithm>
#include <cctype>
#include <cstdlib>

namespace {

// ---------------------------------------------------------------------------
// text helpers
// ---------------------------------------------------------------------------

std::string toLower(const std::string& text) {
    std::string result = text;
    std::transform(result.begin(), result.end(), result.begin(),
                   [](unsigned char c) { return static_cast<char>(std::tolower(c)); });
    return result;
}

std::string trim(const std::string& text) {
    const char* whitespace = " \t\r\n\f\v";
    size_t start = text.find_first_not_of(whitespace);
    if (start == std::string::npos) {
        return "";
    }
    size_t end = text.find_last_not_of(whitespace);
    return text.substr(start, end - start + 1);
}

std::string collapseSpaces(const std::string& text) {
    std::string result;
    bool previousWasSpace = false;
    for (char c : text) {
        bool isSpace = std::isspace(static_cast<unsigned char>(c)) != 0;
        if (isSpace) {
            if (!previousWasSpace && !result.empty()) {
                result += ' ';
            }
        } else {
            result += c;
        }
        previousWasSpace = isSpace;
    }
    return trim(result);
}

void replaceAll(std::string& text, const std::string& from, const std::string& to) {
    if (from.empty()) {
        return;
    }
    size_t position = 0;
    while ((position = text.find(from, position)) != std::string::npos) {
        text.replace(position, from.length(), to);
        position += to.length();
    }
}

// normalizeText repairs the UTF-8 punctuation Tesseract commonly emits, so that
// the ASCII patterns below still match. A real scan produced "INV—1023" with an
// em dash, which no hyphen-based pattern could match. Only this internal working
// copy is rewritten; the raw OCR text reported to the caller is never modified.
std::string normalizeText(const std::string& text) {
    std::string result = text;

    // Dashes: en dash, em dash, hyphen bullet, non-breaking hyphen, minus sign.
    replaceAll(result, "\xE2\x80\x93", "-");
    replaceAll(result, "\xE2\x80\x94", "-");
    replaceAll(result, "\xE2\x80\x90", "-");
    replaceAll(result, "\xE2\x80\x91", "-");
    replaceAll(result, "\xE2\x88\x92", "-");

    // Quotes and apostrophes.
    replaceAll(result, "\xE2\x80\x98", "'");
    replaceAll(result, "\xE2\x80\x99", "'");
    replaceAll(result, "\xE2\x80\x9C", "\"");
    replaceAll(result, "\xE2\x80\x9D", "\"");

    // Non-breaking and thin spaces.
    replaceAll(result, "\xC2\xA0", " ");
    replaceAll(result, "\xE2\x80\x89", " ");
    replaceAll(result, "\xE2\x80\xAF", " ");

    // Currency symbols become a single ASCII marker so that "has a currency
    // symbol" survives as a signal for the amount scoring.
    replaceAll(result, "\xE2\x82\xA6", "$"); // naira
    replaceAll(result, "\xE2\x82\xAC", "$"); // euro
    replaceAll(result, "\xC2\xA3", "$");     // pound
    replaceAll(result, "\xC2\xA5", "$");     // yen
    replaceAll(result, "\xE2\x82\xB9", "$"); // rupee

    return result;
}

std::vector<std::string> splitLines(const std::string& text) {
    std::vector<std::string> lines;
    std::string current;
    for (char c : text) {
        if (c == '\n') {
            lines.push_back(current);
            current.clear();
        } else if (c != '\r') {
            current += c;
        }
    }
    lines.push_back(current);
    return lines;
}

bool containsAny(const std::string& lowerLine, const std::vector<std::string>& needles) {
    for (const auto& needle : needles) {
        if (lowerLine.find(needle) != std::string::npos) {
            return true;
        }
    }
    return false;
}

std::string stripEdgePunctuation(const std::string& text) {
    std::string result = trim(text);
    while (!result.empty()) {
        char c = result.back();
        if (c == '.' || c == ',' || c == ':' || c == ';' || c == '-' || c == '/' || c == '#') {
            result.pop_back();
        } else {
            break;
        }
    }
    while (!result.empty()) {
        char c = result.front();
        if (c == '.' || c == ',' || c == ':' || c == ';' || c == '-' || c == '/' || c == '#') {
            result.erase(result.begin());
        } else {
            break;
        }
    }
    return result;
}

// ---------------------------------------------------------------------------
// date helpers
// ---------------------------------------------------------------------------

int monthFromName(const std::string& name) {
    static const std::vector<std::string> months = {
        "jan", "feb", "mar", "apr", "may", "jun",
        "jul", "aug", "sep", "oct", "nov", "dec"
    };

    std::string lower = toLower(name);
    for (size_t i = 0; i < months.size(); i++) {
        if (lower.compare(0, months[i].size(), months[i]) == 0) {
            return static_cast<int>(i) + 1;
        }
    }
    return 0;
}

int normalizeYear(int year) {
    if (year >= 100) {
        return year;
    }
    // Two digit years: 70-99 are last century, everything else is this one.
    return year < 70 ? 2000 + year : 1900 + year;
}

bool isLeapYear(int year) {
    return (year % 4 == 0 && year % 100 != 0) || (year % 400 == 0);
}

int daysInMonth(int year, int month) {
    static const int days[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};
    if (month < 1 || month > 12) {
        return 0;
    }
    if (month == 2 && isLeapYear(year)) {
        return 29;
    }
    return days[month - 1];
}

// isRealDate rejects impossible values such as 2026-13-45, which the previous
// implementation happily reported.
bool isRealDate(int year, int month, int day) {
    if (year < 1990 || year > 2100) {
        return false;
    }
    if (month < 1 || month > 12) {
        return false;
    }
    return day >= 1 && day <= daysInMonth(year, month);
}

std::string formatISO(int year, int month, int day) {
    std::ostringstream oss;
    oss << std::setfill('0') << std::setw(4) << year << "-"
        << std::setw(2) << month << "-"
        << std::setw(2) << day;
    return oss.str();
}

// ---------------------------------------------------------------------------
// amount helpers
// ---------------------------------------------------------------------------

struct AmountCandidate {
    double value = 0.0;
    std::string text;
    int score = 0;
    bool valid = false;
};

// Money tokens are scored by the label that most closely precedes them rather
// than by the line as a whole, so that a receipt printing several fields on one
// line still resolves each number to the right field.
struct AmountLabel {
    const char* keyword;
    int score; // negative means "this number is an identifier, not money"
};

const std::vector<AmountLabel>& amountLabels() {
    static const std::vector<AmountLabel> labels = {
        // Identifiers and contact details: never amounts.
        {"invoice", -1}, {"inv", -1}, {"receipt no", -1}, {"receipt number", -1},
        {"tel", -1}, {"telephone", -1}, {"phone", -1}, {"mobile", -1}, {"contact", -1},
        {"account no", -1}, {"account number", -1}, {"acct", -1}, {"card", -1},
        {"reference", -1}, {"ref", -1}, {"rrn", -1}, {"trn", -1}, {"txn", -1},
        {"date", -1}, {"vat no", -1}, {"vat number", -1}, {"tin", -1},

        // Amount labels, strongest first.
        {"grand total", 100}, {"total due", 100}, {"amount due", 100},
        {"balance due", 100}, {"net payable", 100}, {"total payable", 100},
        {"amount payable", 100},
        {"subtotal", 30}, {"sub total", 30}, {"sub-total", 30},
        {"total", 90},
        {"amount paid", 85}, {"amount", 70}, {"paid", 85}, {"payment", 85},
        {"vat", 10}, {"tax", 10}, {"discount", 10}, {"change", 10}, {"tender", 10},
    };
    return labels;
}

// nearestLabelScore returns the score of the label ending closest before the
// token. Where two labels overlap, such as "total" inside "subtotal", the longer
// one wins so that a subtotal is not mistaken for the total.
int nearestLabelScore(const std::string& lowerPrefix, int defaultScore) {
    size_t bestEnd = std::string::npos;
    size_t bestLength = 0;
    int bestScore = defaultScore;
    bool found = false;

    for (const auto& label : amountLabels()) {
        std::string keyword(label.keyword);
        size_t position = lowerPrefix.rfind(keyword);
        if (position == std::string::npos) {
            continue;
        }
        // Require the keyword to start at a word boundary.
        if (position > 0 && std::isalnum(static_cast<unsigned char>(lowerPrefix[position - 1]))) {
            continue;
        }
        size_t end = position + keyword.size();

        if (!found || end > bestEnd || (end == bestEnd && keyword.size() > bestLength)) {
            found = true;
            bestEnd = end;
            bestLength = keyword.size();
            bestScore = label.score;
        }
    }

    return bestScore;
}

// parseMoneyToken converts a printed money token into a number, handling both
// the 1,234.56 and the European 1.234,56 conventions.
bool parseMoneyToken(const std::string& token, double& value) {
    std::string digits;
    for (char c : token) {
        if (std::isdigit(static_cast<unsigned char>(c)) || c == '.' || c == ',') {
            digits += c;
        }
    }
    if (digits.empty()) {
        return false;
    }

    size_t lastDot = digits.find_last_of('.');
    size_t lastComma = digits.find_last_of(',');

    std::string decimalPart;
    std::string integerPart = digits;

    auto looksLikeDecimalSeparator = [&](size_t position) {
        if (position == std::string::npos) {
            return false;
        }
        size_t trailing = digits.size() - position - 1;
        // Three trailing digits are a thousands group, not a decimal fraction.
        return trailing >= 1 && trailing <= 2;
    };

    if (lastDot != std::string::npos && lastComma != std::string::npos) {
        size_t separator = std::max(lastDot, lastComma);
        if (looksLikeDecimalSeparator(separator)) {
            integerPart = digits.substr(0, separator);
            decimalPart = digits.substr(separator + 1);
        }
    } else if (looksLikeDecimalSeparator(lastDot)) {
        integerPart = digits.substr(0, lastDot);
        decimalPart = digits.substr(lastDot + 1);
    } else if (looksLikeDecimalSeparator(lastComma)) {
        integerPart = digits.substr(0, lastComma);
        decimalPart = digits.substr(lastComma + 1);
    }

    std::string cleanInteger;
    for (char c : integerPart) {
        if (std::isdigit(static_cast<unsigned char>(c))) {
            cleanInteger += c;
        }
    }
    if (cleanInteger.empty()) {
        return false;
    }

    std::string normalized = cleanInteger;
    if (!decimalPart.empty()) {
        normalized += "." + decimalPart;
    }

    try {
        value = std::stod(normalized);
    } catch (const std::exception&) {
        return false;
    }
    return true;
}

std::string trimTrailingZeros(double value) {
    std::ostringstream oss;
    oss << std::fixed << std::setprecision(2) << value;
    return oss.str();
}

} // namespace

// ---------------------------------------------------------------------------
// construction
// ---------------------------------------------------------------------------

ReceiptParser::ReceiptParser() {
    // Invoice: the label locates the line; the token pattern then reads the
    // identifier itself, including any alphabetic prefix. The old pattern
    // captured (\d+...) directly after the label, so "INV-1023" was reported as
    // "1023" and never matched an invoice stored as "INV-1023".
    invoiceLabelPattern_ = std::regex(
        R"(\b(?:invoice|inv|bill|receipt|purchase\s+order|po|transaction\s+id|txn\s+id)\b\s*(?:number|no\.?|num|#)?)",
        std::regex_constants::icase);

    invoiceTokenPattern_ = std::regex(
        R"(\b([A-Za-z]{1,8}[-/]?\d{2,}[A-Za-z0-9\-/]*|\d{3,}[A-Za-z0-9\-/]*)\b)");

    invoiceStandalonePattern_ = std::regex(R"(\b([A-Za-z]{2,6}[-/]\d{2,}[A-Za-z0-9\-/]*)\b)");

    numericFallbackPattern_ = std::regex(R"(\b(\d{4,})\b)");

    // Money tokens, optionally preceded by the normalised currency marker.
    moneyPattern_ = std::regex(R"((\$\s*)?(\d{1,3}(?:[,.\s]\d{3})+(?:[.,]\d{1,2})?|\d+[.,]\d{1,2}|\d+))");

    isoDatePattern_ = std::regex(R"(\b(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})\b)");
    numericDatePattern_ = std::regex(R"(\b(\d{1,2})[-/.](\d{1,2})[-/.](\d{2,4})\b)");
    dayMonthNamePattern_ = std::regex(R"(\b(\d{1,2})[-\s]+([A-Za-z]{3,9})[-\s,]+(\d{2,4})\b)",
                                      std::regex_constants::icase);
    monthNameDayPattern_ = std::regex(R"(\b([A-Za-z]{3,9})[-\s]+(\d{1,2})(?:st|nd|rd|th)?[,\s]+(\d{2,4})\b)",
                                      std::regex_constants::icase);

    // Reference: the alternatives are ordered longest first and every one is
    // bounded by \b. The old pattern let "ref" match inside "Reference" and then
    // captured the remainder of the word, reporting "erence" as the reference.
    bankRefPattern_ = std::regex(
        R"(\b(?:payment\s+reference|transaction\s+reference|payment\s+ref|transaction\s+ref|reference|ref\s+no|ref|rrn|trn|txn)\b\s*[:#\-]?\s*([A-Za-z0-9][A-Za-z0-9\-/]{3,}))",
        std::regex_constants::icase);

    // Customer: capture to the end of the line only. The old [A-Za-z\s]+ class
    // included newlines, so the value ran on into the following line's label.
    customerNamePattern_ = std::regex(
        R"(\b(?:customer\s+name|customer|bill(?:ed)?\s+to|sold\s+to|pay(?:able)?\s+to|paid\s+by|client|account\s+name)\b\s*[:\-]?[ \t]*([^\r\n]*))",
        std::regex_constants::icase);

    // Phone: capture the number itself rather than the whole match, which used
    // to include the "Tel:" label in the extracted value.
    phonePattern_ = std::regex(
        R"(\b(?:tel(?:ephone)?|phone|mobile|cell|contact)\b\s*[:#\-]?\s*(\+?\d[\d\s().\-]{5,}\d))",
        std::regex_constants::icase);

    emailPattern_ = std::regex(R"([\w._%+\-]+@[\w.\-]+\.[A-Za-z]{2,})");

    LOG_INFO("ReceiptParser initialized with line-oriented patterns");
}

ReceiptParser::~ReceiptParser() {
    LOG_INFO("ReceiptParser destroyed");
}

// ---------------------------------------------------------------------------
// parsing
// ---------------------------------------------------------------------------

ReceiptData ReceiptParser::parseReceipt(const std::string& ocrText, double confidence) {
    LOG_INFO("Parsing receipt text with confidence: " + std::to_string(confidence));

    ReceiptData data;
    data.isValid = false;
    data.confidence = confidence;
    data.requiresManualReview = false;

    // Extract fields
    data.invoiceNumber = extractInvoiceNumber(ocrText);
    std::string amountStr = extractAmount(ocrText);
    data.paymentDate = extractDate(ocrText);
    data.bankReference = extractBankReference(ocrText);
    data.customerName = extractCustomerName(ocrText);

    // Parse amount
    if (!amountStr.empty()) {
        data.amountPaid = parseAmount(amountStr);
    } else {
        data.amountPaid = 0.0;
    }

    // Validate required fields
    bool hasInvoice = isValidInvoiceNumber(data.invoiceNumber);
    bool hasAmount = data.amountPaid > 0;

    // Check if manual review is needed based on confidence threshold
    const double CONFIDENCE_THRESHOLD = 85.0;
    if (confidence > 0 && confidence < CONFIDENCE_THRESHOLD) {
        data.requiresManualReview = true;
        LOG_WARNING("Low OCR confidence (" + std::to_string(confidence) +
                    ") - manual review required");
    }

    if (hasInvoice && hasAmount) {
        data.isValid = true;
        LOG_INFO("Receipt parsed successfully - Invoice: " + data.invoiceNumber +
                 ", Amount: " + std::to_string(data.amountPaid) +
                 ", Confidence: " + std::to_string(confidence) +
                 ", Manual Review: " + (data.requiresManualReview ? "Yes" : "No"));
    } else {
        LOG_WARNING("Receipt validation failed - Invoice: " + data.invoiceNumber +
                    ", Amount: " + std::to_string(data.amountPaid) +
                    ", Date: " + data.paymentDate);
    }

    return data;
}

bool ReceiptParser::isValidInvoiceNumber(const std::string& invoiceNumber) {
    if (invoiceNumber.empty() || invoiceNumber.length() < 3) {
        return false;
    }

    bool hasAlnum = false;
    for (char c : invoiceNumber) {
        if (std::isalnum(static_cast<unsigned char>(c))) {
            hasAlnum = true;
            break;
        }
    }

    return hasAlnum;
}

bool ReceiptParser::isValidAmount(const std::string& amountStr) {
    if (amountStr.empty()) {
        return false;
    }

    double value = 0.0;
    if (!parseMoneyToken(amountStr, value)) {
        return false;
    }
    return value > 0;
}

bool ReceiptParser::isValidDate(const std::string& dateStr) {
    if (dateStr.empty()) {
        return false;
    }
    // A date is valid when it can be normalised to a real calendar date.
    return !formatDate(dateStr).empty();
}

// ---------------------------------------------------------------------------
// invoice number
// ---------------------------------------------------------------------------

std::string ReceiptParser::extractInvoiceNumber(const std::string& text) {
    std::string normalized = normalizeText(text);
    std::vector<std::string> lines = splitLines(normalized);

    // Pass 1: a labelled line. Search from the start of the label so that a bare
    // "INV-1023" (where the label is part of the identifier) is kept whole.
    for (const auto& line : lines) {
        std::smatch labelMatch;
        if (!std::regex_search(line, labelMatch, invoiceLabelPattern_)) {
            continue;
        }

        std::string lowerLine = toLower(line);
        // Amount and contact lines carry numbers that are not identifiers.
        if (containsAny(lowerLine, {"total", "amount", "vat", "tax", "tel", "phone"})) {
            continue;
        }

        std::string fromLabel = line.substr(static_cast<size_t>(labelMatch.position(0)));

        auto begin = std::sregex_iterator(fromLabel.begin(), fromLabel.end(), invoiceTokenPattern_);
        auto end = std::sregex_iterator();
        for (auto it = begin; it != end; ++it) {
            std::string candidate = stripEdgePunctuation((*it)[1].str());
            if (candidate.empty()) {
                continue;
            }
            // Skip anything that is really a date on the same line.
            if (!formatDate(candidate).empty()) {
                continue;
            }
            LOG_DEBUG("Extracted invoice number: " + candidate);
            return candidate;
        }
    }

    // Pass 2: a standalone identifier such as INV-1023 anywhere in the text.
    std::smatch standalone;
    if (std::regex_search(normalized, standalone, invoiceStandalonePattern_)) {
        std::string candidate = stripEdgePunctuation(standalone[1].str());
        if (!candidate.empty() && formatDate(candidate).empty()) {
            LOG_DEBUG("Extracted invoice number: " + candidate);
            return candidate;
        }
    }

    // Pass 3: a bare number, ignoring lines that clearly hold something else.
    for (const auto& line : lines) {
        std::string lowerLine = toLower(line);
        if (containsAny(lowerLine, {"total", "amount", "vat", "tax", "tel", "phone",
                                    "date", "subtotal", "balance", "account"})) {
            continue;
        }
        std::smatch numeric;
        if (std::regex_search(line, numeric, numericFallbackPattern_)) {
            std::string candidate = numeric[1].str();
            if (formatDate(candidate).empty()) {
                LOG_DEBUG("Extracted invoice number: " + candidate);
                return candidate;
            }
        }
    }

    LOG_DEBUG("No invoice number found");
    return "";
}

// ---------------------------------------------------------------------------
// amount
// ---------------------------------------------------------------------------

std::string ReceiptParser::extractAmount(const std::string& text) {
    std::string normalized = normalizeText(text);
    std::vector<std::string> lines = splitLines(normalized);

    AmountCandidate best;

    for (const auto& line : lines) {
        std::string lowerLine = toLower(line);

        auto begin = std::sregex_iterator(line.begin(), line.end(), moneyPattern_);
        auto end = std::sregex_iterator();
        for (auto it = begin; it != end; ++it) {
            const std::smatch& match = *it;
            std::string token = match[2].str();

            double value = 0.0;
            if (!parseMoneyToken(token, value) || value <= 0) {
                continue;
            }

            // Score by the label nearest to this token, not by the whole line:
            // "Invoice: INV-6602 ... Total: 12,500.00" holds both an identifier
            // and an amount.
            size_t tokenStart = static_cast<size_t>(match.position(0));
            int labelScore = nearestLabelScore(lowerLine.substr(0, tokenStart), 5);
            if (labelScore < 0) {
                continue; // the nearest label marks this number as an identifier
            }

            bool hasCurrency = !match[1].str().empty();
            bool hasDecimals = token.find('.') != std::string::npos ||
                               token.find(',') != std::string::npos;

            int score = labelScore;
            if (hasCurrency) {
                score += 10;
            }
            if (hasDecimals) {
                score += 5;
            }
            // A bare integer with no label and no currency is rarely an amount.
            if (!hasCurrency && !hasDecimals && labelScore <= 5) {
                score = 1;
            }

            if (!best.valid || score > best.score ||
                (score == best.score && value > best.value)) {
                best.valid = true;
                best.value = value;
                best.score = score;
                best.text = trimTrailingZeros(value);
            }
        }
    }

    if (best.valid) {
        LOG_DEBUG("Extracted amount: " + best.text);
        return best.text;
    }

    LOG_DEBUG("No amount found");
    return "";
}

// ---------------------------------------------------------------------------
// date
// ---------------------------------------------------------------------------

std::string ReceiptParser::extractDate(const std::string& text) {
    std::string normalized = normalizeText(text);
    std::vector<std::string> lines = splitLines(normalized);

    std::string bestDate;
    int bestScore = -1;

    for (const auto& line : lines) {
        std::string lowerLine = toLower(line);

        int labelScore = 20;
        if (containsAny(lowerLine, {"payment date", "date paid", "paid on",
                                    "transaction date", "value date"})) {
            labelScore = 100;
        } else if (containsAny(lowerLine, {"due date", "expiry", "valid until"})) {
            // The due date belongs to the invoice, not to the payment.
            labelScore = 5;
        } else if (containsAny(lowerLine, {"date"})) {
            labelScore = 80;
        }

        std::string candidate = formatDate(line);
        if (candidate.empty()) {
            continue;
        }

        if (labelScore > bestScore) {
            bestScore = labelScore;
            bestDate = candidate;
        }
    }

    if (!bestDate.empty()) {
        LOG_DEBUG("Extracted date: " + bestDate);
        return bestDate;
    }

    LOG_DEBUG("No date found");
    return "";
}

// formatDate normalises any recognised date to YYYY-MM-DD, and returns an empty
// string when the text holds no real date. ISO is tried first: the old ordering
// put an ambiguous d/m/y pattern ahead of it, so "2026-06-29" was truncated to
// "26-06-29".
std::string ReceiptParser::formatDate(const std::string& dateStr) {
    std::string normalized = normalizeText(dateStr);
    std::smatch match;

    if (std::regex_search(normalized, match, isoDatePattern_)) {
        int year = std::atoi(match[1].str().c_str());
        int month = std::atoi(match[2].str().c_str());
        int day = std::atoi(match[3].str().c_str());
        if (isRealDate(year, month, day)) {
            return formatISO(year, month, day);
        }
    }

    if (std::regex_search(normalized, match, dayMonthNamePattern_)) {
        int day = std::atoi(match[1].str().c_str());
        int month = monthFromName(match[2].str());
        int year = normalizeYear(std::atoi(match[3].str().c_str()));
        if (month != 0 && isRealDate(year, month, day)) {
            return formatISO(year, month, day);
        }
    }

    if (std::regex_search(normalized, match, monthNameDayPattern_)) {
        int month = monthFromName(match[1].str());
        int day = std::atoi(match[2].str().c_str());
        int year = normalizeYear(std::atoi(match[3].str().c_str()));
        if (month != 0 && isRealDate(year, month, day)) {
            return formatISO(year, month, day);
        }
    }

    if (std::regex_search(normalized, match, numericDatePattern_)) {
        int first = std::atoi(match[1].str().c_str());
        int second = std::atoi(match[2].str().c_str());
        int year = normalizeYear(std::atoi(match[3].str().c_str()));

        // Disambiguate day-first from month-first where the values allow it,
        // otherwise assume day-first (DD/MM/YYYY).
        int day = first;
        int month = second;
        if (first > 12 && second <= 12) {
            day = first;
            month = second;
        } else if (second > 12 && first <= 12) {
            day = second;
            month = first;
        }

        if (isRealDate(year, month, day)) {
            return formatISO(year, month, day);
        }
    }

    return "";
}

// ---------------------------------------------------------------------------
// reference
// ---------------------------------------------------------------------------

std::string ReceiptParser::extractBankReference(const std::string& text) {
    std::string normalized = normalizeText(text);
    std::smatch match;

    if (std::regex_search(normalized, match, bankRefPattern_) && match.size() > 1) {
        std::string reference = stripEdgePunctuation(match[1].str());
        reference.erase(std::remove_if(reference.begin(), reference.end(),
                                       [](unsigned char c) { return std::isspace(c) != 0; }),
                        reference.end());
        if (!reference.empty()) {
            LOG_DEBUG("Extracted bank reference: " + reference);
            return reference;
        }
    }

    LOG_DEBUG("No bank reference found");
    return "";
}

// ---------------------------------------------------------------------------
// customer
// ---------------------------------------------------------------------------

std::string ReceiptParser::extractCustomerName(const std::string& text) {
    std::string normalized = normalizeText(text);
    std::vector<std::string> lines = splitLines(normalized);

    for (size_t i = 0; i < lines.size(); i++) {
        std::smatch match;
        if (!std::regex_search(lines[i], match, customerNamePattern_) || match.size() < 2) {
            continue;
        }

        std::string value = collapseSpaces(match[1].str());

        // A label alone on its line means the value is on the next line.
        if (value.empty() && i + 1 < lines.size()) {
            value = collapseSpaces(lines[i + 1]);
        }

        // Single line receipts put several fields on one line; stop at the next
        // label rather than swallowing the rest of the line.
        static const std::vector<std::string> followingLabels = {
            "date", "invoice", "receipt", "tel", "phone", "email", "amount",
            "total", "reference", "ref", "vat", "address"
        };
        std::string lowerValue = toLower(value);
        size_t cut = std::string::npos;
        for (const auto& label : followingLabels) {
            size_t position = lowerValue.find(label);
            while (position != std::string::npos) {
                bool atWordStart = position == 0 ||
                                   !std::isalnum(static_cast<unsigned char>(lowerValue[position - 1]));
                size_t after = position + label.size();
                // Only treat it as a label when it is followed by a separator.
                bool looksLikeLabel = after < lowerValue.size() &&
                                      (lowerValue[after] == ':' || lowerValue[after] == '-' ||
                                       (lowerValue[after] == ' ' && after + 1 < lowerValue.size() &&
                                        lowerValue[after + 1] == ':'));
                if (atWordStart && looksLikeLabel && position > 0) {
                    cut = std::min(cut, position);
                    break;
                }
                position = lowerValue.find(label, position + 1);
            }
        }
        if (cut != std::string::npos) {
            value = value.substr(0, cut);
        }

        value = stripEdgePunctuation(collapseSpaces(value));

        bool hasLetter = false;
        for (char c : value) {
            if (std::isalpha(static_cast<unsigned char>(c))) {
                hasLetter = true;
                break;
            }
        }

        if (hasLetter && value.size() >= 2 && value.size() <= 80) {
            LOG_DEBUG("Extracted customer name: " + value);
            return value;
        }
    }

    LOG_DEBUG("No customer name found");
    return "";
}

// ---------------------------------------------------------------------------
// contact details
// ---------------------------------------------------------------------------

std::string ReceiptParser::extractPhoneNumber(const std::string& text) {
    std::string normalized = normalizeText(text);
    std::smatch match;

    if (std::regex_search(normalized, match, phonePattern_) && match.size() > 1) {
        std::string phone = match[1].str();
        phone.erase(std::remove_if(phone.begin(), phone.end(),
                                   [](unsigned char c) {
                                       return c == '(' || c == ')' || c == '-' ||
                                              c == '.' || std::isspace(c) != 0;
                                   }),
                    phone.end());
        if (!phone.empty()) {
            LOG_DEBUG("Extracted phone number: " + phone);
            return phone;
        }
    }

    LOG_DEBUG("No phone number found");
    return "";
}

std::string ReceiptParser::extractEmail(const std::string& text) {
    std::string normalized = normalizeText(text);
    std::smatch match;

    if (std::regex_search(normalized, match, emailPattern_)) {
        std::string email = match[0].str();
        LOG_DEBUG("Extracted email: " + email);
        return email;
    }

    LOG_DEBUG("No email found");
    return "";
}

// ---------------------------------------------------------------------------
// numbers
// ---------------------------------------------------------------------------

double ReceiptParser::parseAmount(const std::string& amountStr) {
    double value = 0.0;
    if (parseMoneyToken(amountStr, value)) {
        return value;
    }
    LOG_ERROR("Failed to parse amount: " + amountStr);
    return 0.0;
}
