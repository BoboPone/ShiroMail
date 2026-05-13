const RAW_PREVIEW_LIMIT = 128 * 1024;
const RAW_SECTION_LIMIT = 48 * 1024;
const RECEIVED_ENTRY_LIMIT = 2 * 1024;
const HTML_DATA_URI_LIMIT = 256 * 1024;

type MailHeaderMap = Record<string, string[]>;

export type ParsedRawAttachmentLike = {
  contentId: string;
  contentType: string;
};

type HtmlPreviewResult = {
  html: string;
  notices: string[];
};

type RawPreviewResult = {
  headers: string;
  body: string;
  preview: string;
  isTruncated: boolean;
  totalLength: number;
};

export function resolveMessageBody(message: {
  textBody: string;
  htmlBody: string;
  textPreview: string;
  htmlPreview: string;
}) {
  const textBody = message.textBody.trim();
  if (textBody) {
    return textBody;
  }

  const htmlBody = (message.htmlBody || message.htmlPreview).trim();
  if (htmlBody) {
    return htmlBody.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ").trim();
  }

  const preview = message.textPreview.trim();
  if (preview) {
    return preview;
  }

  return "暂无正文内容。";
}

export function resolveHtmlBody(message: {
  htmlBody: string;
  htmlPreview: string;
}) {
  return (message.htmlBody || message.htmlPreview).trim();
}

export function buildMailHtmlPreview(content: string, cidSources: Record<string, string> = {}): HtmlPreviewResult {
  const notices: string[] = [];
  let html = content.trim();

  html = html.replace(/<script\b[\s\S]*?<\/script>/gi, "");
  html = html.replace(/<iframe\b[\s\S]*?<\/iframe>/gi, () => {
    notices.push("已隐藏邮件中的嵌入框架内容。");
    return buildNoticeBlock("已隐藏嵌入内容，请下载原文查看。");
  });
  html = html.replace(/<img\b([^>]*?)\bsrc=(["'])cid:([^"']+)\2([^>]*)>/gi, (_match, beforeSrc, quote, cidValue, afterSrc) => {
    const normalizedCID = normalizeCIDReference(cidValue);
    const resolvedSource = cidSources[normalizedCID];
    if (resolvedSource) {
      return addImageLoadingHints(`<img${beforeSrc ?? ""} src=${quote}${resolvedSource}${quote}${afterSrc ?? ""}>`);
    }
    notices.push(`邮件中的 CID 图片 ${normalizedCID} 暂时无法解析。`);
    const alt = getImageAltText(`${beforeSrc ?? ""} ${afterSrc ?? ""}`);
    return buildNoticeBlock(alt ? `CID 图片未解析：${alt}` : `CID 图片 ${normalizedCID} 暂不支持预览，请下载原文或附件查看。`);
  });
  html = html.replace(/<img\b([^>]*?)\bsrc=(["'])(data:image\/[^"']+)\2([^>]*)>/gi, (...args) => {
    const match = args[0];
    const beforeSrc = args[1] as string;
    const dataUri = args[3] as string;
    const afterSrc = args[4] as string;
    if (dataUri.length <= HTML_DATA_URI_LIMIT) {
      return addImageLoadingHints(match);
    }
    notices.push("已隐藏过大的内联图片，避免邮件预览卡顿。");
    const alt = getImageAltText(`${beforeSrc ?? ""} ${afterSrc ?? ""}`);
    return buildNoticeBlock(alt ? `图片已隐藏：${alt}` : "过大的内联图片已隐藏，请下载原文查看。");
  });
  html = html.replace(/<img\b[^>]*>/gi, (match) => addImageLoadingHints(match));

  return {
    html,
    notices: dedupeNotices(notices),
  };
}

export function buildMailHtmlDocument(content: string) {
  return `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>
      :root { color-scheme: light; }
      * { box-sizing: border-box; max-width: 100%; }
      html, body {
        margin: 0;
        background: #ffffff;
      }
      body {
        padding: 16px;
        color: #111827;
        font: 14px/1.7 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
        word-break: break-word;
        overflow-wrap: anywhere;
      }
      img {
        display: block;
        max-width: 100% !important;
        height: auto !important;
        object-fit: contain;
      }
      pre {
        white-space: pre-wrap;
        overflow-x: auto;
      }
      table {
        display: block;
        width: max-content;
        max-width: 100%;
        overflow-x: auto;
        border-collapse: collapse;
      }
      td, th {
        word-break: break-word;
      }
      blockquote {
        margin: 0;
        padding-left: 12px;
        border-left: 2px solid #d1d5db;
        color: #4b5563;
      }
      .shiro-mail-notice {
        margin: 12px 0;
        padding: 12px;
        border: 1px solid #d1d5db;
        background: #f8fafc;
        color: #475569;
        font-size: 12px;
        line-height: 1.6;
      }
    </style>
  </head>
  <body>${content}</body>
</html>`;
}

export function openHtmlPreviewWindow(content: string) {
  const previewWindow = window.open("", "_blank", "noopener,noreferrer");
  if (!previewWindow) {
    return;
  }
  previewWindow.document.open();
  previewWindow.document.write(buildMailHtmlDocument(content));
  previewWindow.document.close();
}

export function filterHeaderEntries(headers: MailHeaderMap, keyword: string, decodeValue: (value: string) => string) {
  const normalized = keyword.trim().toLowerCase();
  const entries = Object.entries(headers).map(([key, values]) => [
    key,
    values.map((value) => decodeValue(value)),
  ] as const);
  if (!normalized) {
    return entries;
  }
  return entries.filter(([key, values]) =>
    key.toLowerCase().includes(normalized) ||
    values.some((value) => value.toLowerCase().includes(normalized)),
  );
}

export function buildRawPreview(raw: string): RawPreviewResult {
  const normalized = raw.replace(/\r\n/g, "\n");
  const preview = normalized.length > RAW_PREVIEW_LIMIT ? normalized.slice(0, RAW_PREVIEW_LIMIT) : normalized;
  const dividerIndex = preview.indexOf("\n\n");
  const headers = dividerIndex === -1 ? preview.trim() : preview.slice(0, dividerIndex).trim();
  const body = dividerIndex === -1 ? "" : preview.slice(dividerIndex + 2).trim();

  return {
    headers: clampMultiline(headers, RAW_SECTION_LIMIT),
    body: clampMultiline(body, RAW_SECTION_LIMIT),
    preview: clampMultiline(preview.trim(), RAW_PREVIEW_LIMIT),
    isTruncated: normalized.length > RAW_PREVIEW_LIMIT,
    totalLength: normalized.length,
  };
}

export function summarizeMessageHeaders(headers: MailHeaderMap, decodeValue: (value: string) => string) {
  const authResults = getFirstHeaderValue(headers, "Authentication-Results", decodeValue);
  const arcAuthResults = getFirstHeaderValue(headers, "Arc-Authentication-Results", decodeValue);
  const authSource = `${authResults}\n${arcAuthResults}`.trim();

  return {
    spf: extractAuthStatus(authSource, "spf"),
    dkim: extractAuthStatus(authSource, "dkim"),
    dmarc: extractAuthStatus(authSource, "dmarc"),
    replyTo: getFirstHeaderValue(headers, "Reply-To", decodeValue) || "-",
    returnPath: getFirstHeaderValue(headers, "Return-Path", decodeValue) || getFirstHeaderValue(headers, "Envelope-From", decodeValue) || "-",
    messageId: getFirstHeaderValue(headers, "Message-Id", decodeValue) || "-",
  };
}

export function extractReceivedTimeline(headers: MailHeaderMap) {
  const receivedValues = Object.entries(headers)
    .filter(([key]) => key.toLowerCase() === "received")
    .flatMap(([, values]) => values);

  return receivedValues.map((value) => {
    const trimmed = value.trim();
    const parts = trimmed.split(";");
    const route = parts[0]?.replace(/\s+/g, " ").trim() ?? trimmed;
    const date = parts.slice(1).join(";").trim();
    return {
      route: route || "投递链路未知",
      date,
      raw: clampMultiline(trimmed, RECEIVED_ENTRY_LIMIT),
      isRawTruncated: trimmed.length > RECEIVED_ENTRY_LIMIT,
    };
  });
}

export function collectInlineCIDTargets(attachments: ParsedRawAttachmentLike[]) {
  return attachments
    .map((attachment, index) => ({
      attachmentIndex: index,
      contentId: normalizeCIDReference(attachment.contentId),
      contentType: attachment.contentType,
    }))
    .filter((attachment) => attachment.contentId && attachment.contentType.toLowerCase().startsWith("image/"));
}

function getFirstHeaderValue(headers: MailHeaderMap, targetKey: string, decodeValue: (value: string) => string) {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === targetKey.toLowerCase());
  return decodeValue(entry?.[1]?.[0]?.trim() ?? "");
}

function extractAuthStatus(source: string, key: "spf" | "dkim" | "dmarc") {
  if (!source.trim()) {
    return "unknown";
  }
  const match = source.match(new RegExp(`${key}=([a-zA-Z]+)`, "i"));
  return match?.[1]?.toLowerCase() ?? "unknown";
}

function clampMultiline(value: string, limit: number) {
  if (value.length <= limit) {
    return value;
  }
  return `${value.slice(0, limit).trimEnd()}\n\n[内容已截断，剩余部分请下载原文查看]`;
}

function buildNoticeBlock(text: string) {
  return `<div class="shiro-mail-notice">${escapeHtml(text)}</div>`;
}

function escapeHtml(value: string) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function addImageLoadingHints(tag: string) {
  let next = tag;
  if (!/\bloading=/i.test(next)) {
    next = next.replace(/<img/i, '<img loading="lazy"');
  }
  if (!/\bdecoding=/i.test(next)) {
    next = next.replace(/<img/i, '<img decoding="async"');
  }
  if (!/\breferrerpolicy=/i.test(next)) {
    next = next.replace(/<img/i, '<img referrerpolicy="no-referrer"');
  }
  return next;
}

function getImageAltText(attributes: string) {
  const match = attributes.match(/\balt=(["'])(.*?)\1/i);
  return match?.[2]?.trim() ?? "";
}

function dedupeNotices(notices: string[]) {
  return [...new Set(notices)];
}

function normalizeCIDReference(value: string) {
  const normalized = value.trim().replace(/^cid:/i, "").replace(/^<|>$/g, "");
  try {
    return decodeURIComponent(normalized);
  } catch {
    return normalized;
  }
}
