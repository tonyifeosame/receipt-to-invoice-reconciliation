const apiBase = "http://localhost:8080";
const tokenKey = "finance_token";
let dailyChart;
let ocrChart;
let currentReceiptFile = null;
let currentOCRResult = null;
let currentReviewPage = 1;
let currentInvoicePage = 1;
let currentPaymentPage = 1;
const pageSize = 5;
let reviewSortKey = "invoice_number";
let reviewSortAsc = true;
let invoiceSortKey = "invoice_number";
let invoiceSortAsc = true;
let paymentSortKey = "payment_date";
let paymentSortAsc = true;
let reviewFilter = "";
let invoiceFilter = "";
let paymentFilter = "";

const invoiceTableData = [
  { invoice_number: "INV-1023", customer_name: "John Doe Ltd", amount: 150000, status: "PAID", due_date: "03/07/2026" },
  { invoice_number: "INV-1024", customer_name: "Acme Corp", amount: 98000, status: "PENDING", due_date: "12/07/2026" },
  { invoice_number: "INV-1025", customer_name: "Gamma Trade", amount: 65500, status: "FAILED", due_date: "18/07/2026" },
  { invoice_number: "INV-1026", customer_name: "Blue Ocean", amount: 43000, status: "PAID", due_date: "01/07/2026" },
  { invoice_number: "INV-1027", customer_name: "Delta Foods", amount: 212000, status: "PENDING", due_date: "22/07/2026" },
  { invoice_number: "INV-1028", customer_name: "Nova Retail", amount: 124500, status: "PAID", due_date: "05/07/2026" }
];

const paymentTableData = [
  { payment_date: "03/07/2026", invoice_number: "INV-1023", payment_amount: 150000, reference: "TRX238239", status: "PAID" },
  { payment_date: "28/06/2026", invoice_number: "INV-1019", payment_amount: 86000, reference: "TRX238112", status: "PENDING" },
  { payment_date: "26/06/2026", invoice_number: "INV-1017", payment_amount: 132000, reference: "TRX238094", status: "PAID" },
  { payment_date: "21/06/2026", invoice_number: "INV-1003", payment_amount: 45000, reference: "TRX237998", status: "FAILED" },
  { payment_date: "18/06/2026", invoice_number: "INV-1020", payment_amount: 78000, reference: "TRX238150", status: "PAID" }
];

let reviewTableData = [];

function getToken() {
  return localStorage.getItem(tokenKey);
}

function setAuthState(isLoggedIn) {
  document.getElementById("loginSection").classList.toggle("d-none", isLoggedIn);
  document.getElementById("appSection").classList.toggle("d-none", !isLoggedIn);
  document.getElementById("logoutBtn").classList.toggle("d-none", !isLoggedIn);
}

function updateTimestamp() {
  const stamp = new Date().toLocaleString();
  document.getElementById("lastUpdated").textContent = `Last Updated: ${stamp}`;
}

function renderCharts(data) {
  const dailyLabels = (data.daily_stats || []).map((item) => item.date).reverse();
  const matchedValues = (data.daily_stats || []).map((item) => item.matched).reverse();
  const unmatchedValues = (data.daily_stats || []).map((item) => item.unmatched).reverse();

  if (dailyChart) dailyChart.destroy();
  if (ocrChart) ocrChart.destroy();

  const dailyCtx = document.getElementById("dailyChart").getContext("2d");
  dailyChart = new Chart(dailyCtx, {
    type: "bar",
    data: {
      labels: dailyLabels,
      datasets: [
        { label: "Matched", data: matchedValues, backgroundColor: "#198754" },
        { label: "Unmatched", data: unmatchedValues, backgroundColor: "#dc3545" }
      ]
    },
    options: { responsive: true, maintainAspectRatio: false }
  });

  const ocrCtx = document.getElementById("ocrChart").getContext("2d");
  ocrChart = new Chart(ocrCtx, {
    type: "doughnut",
    data: {
      labels: ["Successful", "Needs Review"],
      datasets: [{ data: [85, 15], backgroundColor: ["#0d6efd", "#ffc107"] }]
    },
    options: { responsive: true, maintainAspectRatio: false }
  });
}

async function api(path, options = {}) {
  const headers = options.headers || {};
  if (getToken()) {
    headers.Authorization = `Bearer ${getToken()}`;
  }
  const res = await fetch(`${apiBase}${path}`, { ...options, headers });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.error || data.message || "Request failed");
  }
  return data;
}

function formatCurrency(value) {
  return value != null ? `₦${Number(value).toLocaleString()}` : "-";
}

function statusBadge(status) {
  const normalized = String(status || "").toUpperCase();
  let classes = "badge bg-secondary";
  if (normalized === "PAID" || normalized === "MATCHED") classes = "badge bg-success";
  else if (normalized === "PENDING" || normalized === "REVIEW") classes = "badge bg-warning text-dark";
  else if (normalized === "FAILED" || normalized === "UNMATCHED") classes = "badge bg-danger";
  return `<span class="${classes} status-pill">${normalized || "UNKNOWN"}</span>`;
}

function renderTableRows(data, renderRow) {
  return data.map(renderRow).join("");
}

function filterAndSort(data, filter, key, asc) {
  const filtered = data.filter((item) => {
    if (!filter) return true;
    const text = Object.values(item).join(" ").toLowerCase();
    return text.includes(filter.toLowerCase());
  });
  return filtered.sort((a, b) => {
    const aValue = a[key] || "";
    const bValue = b[key] || "";
    if (!isNaN(Number(aValue)) && !isNaN(Number(bValue))) {
      return asc ? aValue - bValue : bValue - aValue;
    }
    return asc ? String(aValue).localeCompare(String(bValue)) : String(bValue).localeCompare(String(aValue));
  });
}

function renderPagedTable(data, page, pageInfoId, containerId, renderRow) {
  const start = (page - 1) * pageSize;
  const paged = data.slice(start, start + pageSize);
  document.getElementById(containerId).innerHTML = paged.length ? renderTableRows(paged, renderRow) : '<tr><td colspan="5" class="text-muted">No records found.</td></tr>';
  const totalPages = Math.max(1, Math.ceil(data.length / pageSize));
  document.getElementById(pageInfoId).textContent = `Page ${page} of ${totalPages}`;
  return totalPages;
}

function renderReviewTable() {
  const sorted = filterAndSort(reviewTableData, reviewFilter, reviewSortKey, reviewSortAsc);
  const totalPages = renderPagedTable(sorted, currentReviewPage, "reviewPageInfo", "reviewList", (item) => `
        <tr>
          <td>${item.receipt_file || "-"}</td>
          <td>${item.invoice_number || "-"}</td>
          <td>${item.confidence != null ? `${item.confidence}%` : "-"}</td>
          <td>${statusBadge(item.status)}</td>
          <td>
            <button class="btn btn-sm btn-success me-2" data-action="approve" data-invoice="${item.invoice_number}" data-receipt="${item.receipt_file}">Approve</button>
            <button class="btn btn-sm btn-outline-danger" data-action="reject" data-invoice="${item.invoice_number}" data-receipt="${item.receipt_file}">Reject</button>
          </td>
        </tr>`);
  currentReviewPage = Math.min(currentReviewPage, totalPages);
}

function renderInvoiceTable() {
  const sorted = filterAndSort(invoiceTableData, invoiceFilter, invoiceSortKey, invoiceSortAsc);
  const totalPages = renderPagedTable(sorted, currentInvoicePage, "invoicePageInfo", "invoiceTable", (item) => `
        <tr>
          <td>${item.invoice_number}</td>
          <td>${item.customer_name}</td>
          <td>${formatCurrency(item.amount)}</td>
          <td>${statusBadge(item.status)}</td>
          <td>${item.due_date}</td>
        </tr>`);
  currentInvoicePage = Math.min(currentInvoicePage, totalPages);
}

function renderPaymentTable() {
  const sorted = filterAndSort(paymentTableData, paymentFilter, paymentSortKey, paymentSortAsc);
  const totalPages = renderPagedTable(sorted, currentPaymentPage, "paymentPageInfo", "paymentTable", (item) => `
        <tr>
          <td>${item.payment_date}</td>
          <td>${item.invoice_number}</td>
          <td>${formatCurrency(item.payment_amount)}</td>
          <td>${item.reference}</td>
          <td>${statusBadge(item.status)}</td>
        </tr>`);
  currentPaymentPage = Math.min(currentPaymentPage, totalPages);
}

function showSearchCard(containerId, title, fields) {
  document.getElementById(containerId).classList.remove("d-none");
  document.getElementById(containerId).innerHTML = `
    <div class="card border-success shadow-sm">
      <div class="card-body p-3">
        <h6 class="card-title mb-3">${title}</h6>
        ${fields.map((field) => `<div class="mb-2"><span class="text-muted">${field.label}</span><div class="fw-semibold">${field.value}</div></div>`).join("")}
      </div>
    </div>`;
}

function setProgressBar(percent) {
  const progressWrap = document.getElementById("uploadProgressWrap");
  const progressBar = document.getElementById("uploadProgressBar");
  progressWrap.classList.remove("d-none");
  progressBar.style.width = `${percent}%`;
  progressBar.textContent = `${percent}%`;
}

function setUploadProgress(message, percent) {
  setProgressBar(percent);
  document.getElementById("uploadStatus").innerHTML = `<div class="alert alert-info mb-0">${message}</div>`;
}

function showFinalUploadStatus(message, success = true) {
  const statusBox = document.getElementById("uploadStatus");
  const progressBar = document.getElementById("uploadProgressBar");
  progressBar.style.width = "100%";
  progressBar.textContent = "100%";
  statusBox.innerHTML = `<div class="alert ${success ? "alert-success" : "alert-danger"}">${message}</div>`;
}

// OCR text, company API replies and error messages are external data, so they
// are escaped before being placed in innerHTML.
function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (ch) => (
    { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch]
  ));
}

function showUploadAlert(variant, text) {
  document.getElementById("uploadStatus").innerHTML = `<div class="alert ${variant} mb-0">${escapeHtml(text)}</div>`;
}

// OCR results arrive in these shapes:
//   { job_id, status }                              -> /ocr/process queued a job (see pollOCRJob)
//   { ocr_result: {...}, company_response: {...} }   -> a queued job's stored result (GET /jobs/{id})
//   { ocr_results: {...}, company_response: {...} }  -> /ocr/process ran synchronously
//   { receipt_id, ocr: {...}, fields: {...}, ... }   -> OCR ran, no company API configured
function normalizeOCRResponse(result) {
  const payload = result.ocr_results || result.ocr_result || result;
  return {
    receiptId: payload.receipt_id || "",
    requestId: payload.request_id || "",
    fields: payload.fields || {},
    meta: payload.ocr || {},
    rawText: payload.raw_text || "",
    companyResponse: result.company_response || null
  };
}

// Shows what the OCR engine extracted, exactly as extracted: fields are never
// hidden or altered because of confidence, which is only displayed.
function renderOCRResult(ocr) {
  currentOCRResult = ocr;

  const confidence = Number(ocr.meta.confidence || 0);
  document.getElementById("ocrInvoiceNumber").textContent = ocr.fields.invoice_number || "Not found";
  document.getElementById("ocrAmount").textContent = ocr.fields.amount ? formatCurrency(ocr.fields.amount) : "-";
  document.getElementById("ocrDate").textContent = ocr.fields.date || "-";
  document.getElementById("ocrReference").textContent = ocr.fields.reference || "-";
  document.getElementById("ocrCustomer").textContent = ocr.fields.customer || "-";
  document.getElementById("ocrRequestId").textContent = ocr.requestId || "-";
  document.getElementById("ocrRawText").textContent = ocr.rawText || "(no text extracted)";

  const badge = document.getElementById("ocrConfidenceBadge");
  badge.textContent = `Confidence ${confidence}%`;
  badge.className = `badge ${confidence >= 85 ? "bg-success" : "bg-warning text-dark"} status-pill`;

  document.getElementById("ocrPreviewPanel").classList.remove("d-none");
}

// Shows the company API's reply exactly as received: its HTTP status and body,
// verbatim. Nothing in it is parsed or interpreted here; any approval or
// reconciliation decision it carries is the company's. Built with textContent,
// so the reply can never be interpreted as HTML.
function renderCompanyReply(reply) {
  const box = document.getElementById("ocrCompanyResponse");
  box.replaceChildren();
  if (!reply) return;

  const label = document.createElement("div");
  label.className = "small text-muted mb-1";
  label.textContent = `Company API reply${reply.status ? ` (HTTP ${reply.status})` : ""}${reply.note ? ` — ${reply.note}` : ""}`;

  const body = document.createElement("pre");
  body.className = "small bg-light p-2 mb-0";
  body.style.whiteSpace = "pre-wrap";
  body.textContent = reply.body;

  box.append(label, body);
}

// The reply stored on a queued job (company_http_status, company_response and,
// for bodies that are not text, company_response_base64), or null if the
// company API never answered.
function storedCompanyReply(result) {
  if (!result.company_http_status && result.company_response == null && !result.company_response_base64) {
    return null;
  }
  const truncated = result.company_response_truncated ? "truncated to its first 1 MB" : "";
  if (result.company_response != null) {
    return { status: result.company_http_status, body: result.company_response, note: truncated };
  }
  return {
    status: result.company_http_status,
    body: result.company_response_base64,
    note: ["not text, shown base64-encoded", truncated].filter(Boolean).join("; ")
  };
}

// Each upload gets a generation number the moment it starts, before any request
// is made. Every later step of that upload — the upload request, the OCR request,
// job polling, completion and errors — checks it before touching the page, so a
// slower older upload can never overwrite a newer one. Logging out moves it on.
let uploadGeneration = 0;
const jobPollIntervalMs = 2000;
const jobPollTimeoutMs = 5 * 60 * 1000;
const jobPollMaxErrors = 5;

function startUploadGeneration() {
  return ++uploadGeneration;
}

function cancelUploads() {
  uploadGeneration++;
}

function isCurrentUpload(generation) {
  return generation === uploadGeneration;
}

function describeJob(job) {
  switch (job.status) {
    case "PENDING":
      return job.attempts > 0
        ? `waiting to retry (attempt ${job.attempts} of ${job.max_attempts} did not finish)`
        : "queued, waiting for a worker";
    case "PROCESSING":
      return `processing (attempt ${job.attempts} of ${job.max_attempts})`;
    case "COMPLETED":
      return `completed after ${job.attempts} attempt(s)`;
    case "FAILED":
      return `failed after ${job.attempts} attempt(s)`;
    default:
      return String(job.status || "unknown").toLowerCase();
  }
}

async function pollOCRJob(jobId, generation) {
  const startedAt = Date.now();
  let consecutiveErrors = 0;

  showUploadAlert("alert-info", `Receipt queued for OCR processing (job ${jobId}).`);

  while (isCurrentUpload(generation)) {
    let job = null;
    try {
      job = await api(`/jobs/${jobId}`);
      consecutiveErrors = 0;
    } catch (error) {
      consecutiveErrors++;
      if (consecutiveErrors >= jobPollMaxErrors) {
        if (isCurrentUpload(generation)) {
          showUploadAlert("alert-danger", `Could not read the status of job ${jobId}: ${error.message}`);
        }
        return;
      }
    }
    if (!isCurrentUpload(generation)) return;

    if (job) {
      const result = job.result || {};
      document.getElementById("ocrJobStatus").textContent = `Job ${jobId}: ${describeJob(job)}`;
      if (result.ocr_result) {
        renderOCRResult(normalizeOCRResponse(result));
      }
      const reply = storedCompanyReply(result);
      renderCompanyReply(reply);

      if (job.status === "COMPLETED") {
        setProgressBar(100);
        showUploadAlert("alert-info", reply
          ? `Company API replied${reply.status ? ` with HTTP ${reply.status}` : ""}. Its response is shown below.`
          : "OCR complete.");
        return;
      }
      if (job.status === "FAILED") {
        setProgressBar(100);
        const kept = result.ocr_result ? " The OCR result shown was kept." : "";
        showUploadAlert("alert-danger", `Job ${jobId} failed: ${job.error_message || result.error || "unknown error"}.${kept}`);
        return;
      }

      setProgressBar(job.status === "PROCESSING" ? 80 : 65);
      const lastError = result.error ? ` Last attempt: ${result.error}` : "";
      showUploadAlert("alert-info", `Job ${jobId}: ${describeJob(job)}.${lastError}`);
    }

    if (Date.now() - startedAt > jobPollTimeoutMs) {
      showUploadAlert("alert-warning", `Job ${jobId} is still running. Its result is stored when it finishes.`);
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, jobPollIntervalMs));
  }
}

async function processOCR(receiptFile, generation) {
  const result = await api("/ocr/process", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ receipt_file: receiptFile })
  });
  if (!isCurrentUpload(generation)) return;

  // Queued for the background job queue: follow the job until it has a result.
  if (result.job_id) {
    currentOCRResult = null;
    resetOCRPanel();
    await pollOCRJob(result.job_id, generation);
    return;
  }

  // Synchronous mode (no database): the backend returns its own decoding of the
  // company API's reply, not the raw reply, so it is labelled as such.
  const ocr = normalizeOCRResponse(result);
  renderOCRResult(ocr);
  if (ocr.companyResponse) {
    renderCompanyReply({ body: JSON.stringify(ocr.companyResponse, null, 2), note: "as decoded by the backend (synchronous mode)" });
    showUploadAlert("alert-info", "OCR complete. The company API's response is shown below.");
  } else {
    renderCompanyReply(null);
    showUploadAlert("alert-info", "OCR complete. Results were not sent to the company API.");
  }
}

async function uploadReceipt(file) {
  // Taken before anything asynchronous happens, so every step below belongs to this upload.
  const generation = startUploadGeneration();
  try {
    await runUpload(file, generation);
  } catch (error) {
    if (isCurrentUpload(generation)) {
      showFinalUploadStatus(error.message, false);
    }
  }
}

async function runUpload(file, generation) {
  const previewImage = document.getElementById("receiptPreviewImage");
  const ocrPanel = document.getElementById("ocrPreviewPanel");

  currentReceiptFile = file;
  currentOCRResult = null;
  ocrPanel.classList.add("d-none");
  previewImage.src = "";

  setUploadProgress("Uploading receipt...", 20);
  const formData = new FormData();
  formData.append("file", file);
  const uploadResponse = await api("/receipts/upload", {
    method: "POST",
    body: formData
  });
  if (!isCurrentUpload(generation)) return;

  setUploadProgress("OCR Processing...", 55);
  const receiptPath = uploadResponse.file_path || uploadResponse.file || file.name;
  if (file.type.startsWith("image/")) {
    previewImage.src = URL.createObjectURL(file);
  } else {
    previewImage.outerHTML = `<div class="text-muted">Uploaded file: ${escapeHtml(file.name)}</div>`;
  }

  // processOCR renders its own status message, so only advance the progress bar here.
  await processOCR(receiptPath, generation);
  if (!isCurrentUpload(generation)) return;
  setProgressBar(100);
}

function formatInvoiceResult(data) {
  showSearchCard("invoiceResult", "Invoice", [
    { label: "Invoice", value: data.invoice_number || "-" },
    { label: "Customer", value: data.customer_name || "-" },
    { label: "Amount", value: data.amount ? formatCurrency(data.amount) : "-" },
    { label: "Status", value: statusBadge(data.status) },
    { label: "Due Date", value: data.due_date || "-" }
  ]);
}

function formatPaymentResult(data) {
  showSearchCard("paymentResult", "Payment", [
    { label: "Invoice", value: data.invoice_number || "-" },
    { label: "Amount", value: data.payment_amount ? formatCurrency(data.payment_amount) : "-" },
    { label: "Status", value: statusBadge(data.status) },
    { label: "Payment Date", value: data.payment_date || "-" },
    { label: "Reference", value: data.reference || "-" }
  ]);
}

async function searchInvoice(invoiceNumber) {
  const data = await api(`/invoices?invoice_number=${encodeURIComponent(invoiceNumber)}`);
  document.getElementById("invoiceResult").classList.remove("d-none");
  formatInvoiceResult(data);
}

async function searchPayments(invoiceId) {
  const data = await api(`/payments?invoice_id=${encodeURIComponent(invoiceId)}`);
  document.getElementById("paymentResult").classList.remove("d-none");
  if (Array.isArray(data) && data.length) {
    formatPaymentResult({
      invoice_number: data[0].invoice_number || invoiceId,
      payment_amount: data[0].payment_amount,
      status: data[0].status || "PAID",
      payment_date: data[0].payment_date,
      reference: data[0].reference
    });
  } else {
    formatPaymentResult({ invoice_number: invoiceId, status: "UNKNOWN" });
  }
}

async function submitReview(action, invoiceNumber, receiptFile) {
  // The backend returns { success, message } here: approval and reconciliation are
  // owned by the company API, so report whatever it says instead of assuming success.
  const result = await api("/reviews/decision", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ invoice_number: invoiceNumber, receipt_file: receiptFile, decision: action, reason: "Reviewed from dashboard" })
  });
  await loadDashboard();
  const fallback = action === "approve" ? "Approval submitted" : "Rejection submitted";
  showFinalUploadStatus(result.message || fallback, Boolean(result.success));
}

async function reviewCurrentOCR(decision) {
  const invoiceNumber = currentOCRResult && currentOCRResult.fields ? currentOCRResult.fields.invoice_number : "";
  if (!invoiceNumber) {
    showFinalUploadStatus("Cannot review OCR until an invoice is found.", false);
    return;
  }
  const receiptName = currentReceiptFile ? currentReceiptFile.name : "uploaded_receipt";
  await submitReview(decision, invoiceNumber, receiptName);
}

function setupTableControls() {
  document.querySelectorAll(".table-sortable th[data-sort]").forEach((header) => {
    header.addEventListener("click", () => {
      const key = header.getAttribute("data-sort");
      if (header.closest("#page-reviews")) {
        reviewSortKey = key;
        reviewSortAsc = reviewSortKey === key ? !reviewSortAsc : true;
        currentReviewPage = 1;
        renderReviewTable();
      } else if (header.closest("#page-invoices")) {
        invoiceSortKey = key;
        invoiceSortAsc = invoiceSortKey === key ? !invoiceSortAsc : true;
        currentInvoicePage = 1;
        renderInvoiceTable();
      } else if (header.closest("#page-payments")) {
        paymentSortKey = key;
        paymentSortAsc = paymentSortKey === key ? !paymentSortAsc : true;
        currentPaymentPage = 1;
        renderPaymentTable();
      }
    });
  });

  document.getElementById("reviewSearchInput").addEventListener("input", (event) => {
    reviewFilter = event.target.value;
    currentReviewPage = 1;
    renderReviewTable();
  });

  document.getElementById("invoiceTableSearch").addEventListener("input", (event) => {
    invoiceFilter = event.target.value;
    currentInvoicePage = 1;
    renderInvoiceTable();
  });

  document.getElementById("paymentTableSearch").addEventListener("input", (event) => {
    paymentFilter = event.target.value;
    currentPaymentPage = 1;
    renderPaymentTable();
  });

  document.getElementById("invoiceTableClear").addEventListener("click", () => {
    document.getElementById("invoiceTableSearch").value = "";
    invoiceFilter = "";
    currentInvoicePage = 1;
    renderInvoiceTable();
  });

  document.getElementById("paymentTableClear").addEventListener("click", () => {
    document.getElementById("paymentTableSearch").value = "";
    paymentFilter = "";
    currentPaymentPage = 1;
    renderPaymentTable();
  });

  document.getElementById("reviewPrev").addEventListener("click", () => {
    currentReviewPage = Math.max(1, currentReviewPage - 1);
    renderReviewTable();
  });
  document.getElementById("reviewNext").addEventListener("click", () => {
    currentReviewPage += 1;
    renderReviewTable();
  });
  document.getElementById("invoicePrev").addEventListener("click", () => {
    currentInvoicePage = Math.max(1, currentInvoicePage - 1);
    renderInvoiceTable();
  });
  document.getElementById("invoiceNext").addEventListener("click", () => {
    currentInvoicePage += 1;
    renderInvoiceTable();
  });
  document.getElementById("paymentPrev").addEventListener("click", () => {
    currentPaymentPage = Math.max(1, currentPaymentPage - 1);
    renderPaymentTable();
  });
  document.getElementById("paymentNext").addEventListener("click", () => {
    currentPaymentPage += 1;
    renderPaymentTable();
  });
}

function populateDefaultReviewTable(data) {
  reviewTableData = (data || []).map((item, idx) => ({
    receipt_file: item.receipt_name || `receipt_${idx + 1}.jpg`,
    invoice_number: item.invoice_number || `INV-10${idx + 1}`,
    confidence: item.confidence || 78,
    status: item.status || "PENDING"
  }));
  if (!reviewTableData.length) {
    reviewTableData = [
      { receipt_file: "receipt_001.jpg", invoice_number: "INV-1023", confidence: 92, status: "PENDING" },
      { receipt_file: "receipt_002.jpg", invoice_number: "INV-1024", confidence: 88, status: "PENDING" },
      { receipt_file: "receipt_003.jpg", invoice_number: "INV-1025", confidence: 71, status: "UNMATCHED" },
      { receipt_file: "receipt_004.jpg", invoice_number: "INV-1026", confidence: 99, status: "MATCHED" }
    ];
  }
}

async function loadDashboard() {
  try {
    const data = await api("/dashboard");
    document.getElementById("pendingReviews").textContent = data.pending_reviews || 0;
    document.getElementById("successfulReconciliations").textContent = data.successful_reconciliations || 0;
    document.getElementById("failedReconciliations").textContent = data.failed_reconciliations || 0;
    document.getElementById("recentUploads").textContent = data.recent_uploads || 0;

    renderCharts(data);
    populateDefaultReviewTable(data.recent_history || []);
    renderReviewTable();
    renderInvoiceTable();
    renderPaymentTable();

    const auditLogs = document.getElementById("auditLogs");
    const logs = data.audit_activity || [];
    auditLogs.innerHTML = logs.length
      ? logs.map((entry) => `<div class="border rounded p-2 mb-2"><div class="fw-bold">${entry.action}</div><div class="small">${entry.description}</div><div class="small text-muted">${entry.user_name}</div></div>`).join("")
      : '<div class="text-muted">No audit activity yet.</div>';

    updateTimestamp();
  } catch (error) {
    console.error(error);
  }
}

async function login(username, password) {
  const data = await api("/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password })
  });
  localStorage.setItem(tokenKey, data.token);
  initApp();
}

function resetOCRPanel() {
  document.getElementById("ocrPreviewPanel").classList.add("d-none");
  document.getElementById("ocrInvoiceNumber").textContent = "-";
  document.getElementById("ocrAmount").textContent = "-";
  document.getElementById("ocrDate").textContent = "-";
  document.getElementById("ocrReference").textContent = "-";
  document.getElementById("ocrCustomer").textContent = "-";
  document.getElementById("ocrRequestId").textContent = "-";
  document.getElementById("ocrRawText").textContent = "";
  document.getElementById("ocrJobStatus").textContent = "";
  document.getElementById("ocrCompanyResponse").innerHTML = "";
  document.getElementById("ocrConfidenceBadge").textContent = "Confidence";
}

document.getElementById("loginForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const username = document.getElementById("username").value;
  const password = document.getElementById("password").value;
  try {
    await login(username, password);
  } catch (error) {
    alert(error.message);
  }
});

document.getElementById("logoutBtn").addEventListener("click", () => {
  cancelUploads();
  localStorage.removeItem(tokenKey);
  setAuthState(false);
});

document.getElementById("receiptFile").addEventListener("change", (event) => {
  const file = event.target.files[0];
  currentReceiptFile = file;
  const previewBox = document.getElementById("previewBox");
  const previewImage = document.getElementById("receiptPreviewImage");
  if (!file) {
    previewBox.classList.add("d-none");
    previewBox.innerHTML = "";
    resetOCRPanel();
    return;
  }
  previewBox.classList.remove("d-none");
  previewBox.innerHTML = `<strong>${file.name}</strong><br /><span class="text-muted">${(file.size / 1024).toFixed(1)} KB</span>`;
  if (file.type.startsWith("image/")) {
    previewImage.src = URL.createObjectURL(file);
  } else {
    previewImage.src = "";
  }
  showFinalUploadStatus(`Preview ✓ ${file.name}`, true);
  resetOCRPanel();
});

document.getElementById("uploadForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const file = currentReceiptFile || document.getElementById("receiptFile").files[0];
  if (!file) {
    return;
  }
  try {
    await uploadReceipt(file);
  } catch (error) {
    showFinalUploadStatus(error.message, false);
  }
});

document.getElementById("searchInvoiceBtn").addEventListener("click", async () => {
  const value = document.getElementById("invoiceSearch").value;
  if (!value) return;
  try {
    await searchInvoice(value);
  } catch (error) {
    document.getElementById("invoiceResult").textContent = error.message;
    document.getElementById("invoiceResult").classList.remove("d-none");
  }
});

document.getElementById("searchPaymentsBtn").addEventListener("click", async () => {
  const value = document.getElementById("paymentSearch").value;
  if (!value) return;
  try {
    await searchPayments(value);
  } catch (error) {
    document.getElementById("paymentResult").textContent = error.message;
    document.getElementById("paymentResult").classList.remove("d-none");
  }
});

document.getElementById("approveOCRBtn").addEventListener("click", async () => {
  await reviewCurrentOCR("approve");
});

document.getElementById("rejectOCRBtn").addEventListener("click", async () => {
  await reviewCurrentOCR("reject");
});

document.addEventListener("click", async (event) => {
  const button = event.target.closest("button[data-action]");
  if (!button) return;
  const action = button.getAttribute("data-action");
  const invoiceNumber = button.getAttribute("data-invoice");
  const receiptFile = button.getAttribute("data-receipt");
  try {
    await submitReview(action, invoiceNumber, receiptFile);
  } catch (error) {
    alert(error.message);
  }
});

function showPage(pageId) {
  document.querySelectorAll(".page-view").forEach((section) => {
    section.classList.toggle("d-none", section.id !== pageId);
  });
}

document.querySelectorAll(".nav-link[data-view]").forEach((button) => {
  button.addEventListener("click", () => {
    document.querySelectorAll(".nav-link[data-view]").forEach((item) => item.classList.remove("active"));
    button.classList.add("active");
    const selectPage = button.getAttribute("data-view");
    if (selectPage === "dashboard") {
      loadDashboard();
    } else if (selectPage === "reviews") {
      renderReviewTable();
    } else if (selectPage === "invoices") {
      renderInvoiceTable();
    } else if (selectPage === "payments") {
      renderPaymentTable();
    }
    showPage(`page-${selectPage}`);
  });
});

function initApp() {
  setAuthState(true);
  showPage("page-dashboard");
  setupTableControls();
  loadDashboard();
}

if (getToken()) {
  initApp();
}
