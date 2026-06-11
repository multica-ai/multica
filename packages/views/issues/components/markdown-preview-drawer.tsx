"use client";

import { useState, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { Button } from "@multica/ui/components/ui/button";
import { Download, FileText, Loader2, X } from "lucide-react";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { api } from "@multica/core/api";
import { toast } from "sonner";
import { ReadonlyContent } from "../../editor";

interface MarkdownPreviewDrawerProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	content: string;
	title: string;
	issueId?: string;
	issueIdentifier?: string;
	showCommentExportOption?: boolean;
}

export function MarkdownPreviewDrawer({
	open,
	onOpenChange,
	content,
	title,
	issueId,
	issueIdentifier,
	showCommentExportOption = false,
}: MarkdownPreviewDrawerProps) {
	const [exportingPdf, setExportingPdf] = useState(false);
	const [includeComments, setIncludeComments] = useState(false);
	const contentRef = useRef<HTMLDivElement>(null);

	useEffect(() => {
		if (!open) return;
		const handler = (e: KeyboardEvent) => {
			if (e.key === "Escape") onOpenChange(false);
		};
		document.addEventListener("keydown", handler);
		return () => document.removeEventListener("keydown", handler);
	}, [open, onOpenChange]);

	const handleExportMD = () => {
		try {
			const blob = new Blob([content], { type: "text/markdown;charset=utf-8" });
			const url = URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			a.download = `issue-${issueIdentifier || "export"}.md`;
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
			toast.success("Markdown 导出成功");
		} catch (err) {
			toast.error("Markdown 导出失败");
		}
	};

	const handleExportPDF = async () => {
		if (!issueId) {
			toast.error("无法导出 PDF：未指定 Issue ID");
			return;
		}

		// Capture the rendered HTML from the preview content area
		const htmlContent = contentRef.current?.innerHTML;
		if (!htmlContent) {
			toast.error("无法导出 PDF：预览内容为空");
			return;
		}

		setExportingPdf(true);
		try {
			// If comments export is requested, fall back to the original endpoint
			// which fetches comments server-side
			let blob: Blob;
			if (includeComments) {
				blob = await api.exportIssue(issueId, {
					format: "pdf",
					include_comments: true,
				});
			} else {
				// Use the new HTML-based export for exact preview fidelity
				blob = await api.exportIssueHTML(issueId, htmlContent);
			}

			const url = URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			a.download = `issue-${issueIdentifier || "export"}.pdf`;
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
			toast.success("PDF 导出成功");
		} catch (err: any) {
			toast.error(`PDF 导出失败: ${err.message || err}`);
		} finally {
			setExportingPdf(false);
		}
	};

	if (!open || typeof document === "undefined") return null;

	return createPortal(
		<div
			className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
			onClick={() => onOpenChange(false)}
			role="dialog"
			aria-modal="true"
			aria-label={`Markdown 预览：${title}`}
		>
			<div
				className="flex h-[min(90vh,calc(100vh-2rem))] w-full max-w-6xl flex-col overflow-hidden rounded-lg bg-background shadow-xl border border-border"
				onClick={(e) => e.stopPropagation()}
			>
				{/* Header */}
				<div className="flex items-center gap-2 border-b border-border bg-muted/30 px-4 py-2">
					<FileText className="size-4 shrink-0 text-primary" />
					<p className="truncate text-sm font-medium">Markdown 预览：{title}</p>
					<div className="ml-auto flex items-center gap-1">
						<button
							type="button"
							className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
							title="关闭"
							aria-label="关闭"
							onClick={() => onOpenChange(false)}
						>
							<X className="size-4" />
						</button>
					</div>
				</div>

				{/* Toolbar */}
				<div className="px-4 py-2 bg-muted/20 border-b flex flex-wrap items-center justify-between gap-3">
					<div className="flex items-center gap-2">
						<Button
							variant="outline"
							size="sm"
							onClick={handleExportMD}
							className="flex items-center gap-1.5 h-7 text-xs"
						>
							<Download className="h-3.5 w-3.5" />
							导出 MD
						</Button>

						{issueId && (
							<Button
								variant="default"
								size="sm"
								disabled={exportingPdf}
								onClick={handleExportPDF}
								className="flex items-center gap-1.5 h-7 text-xs"
							>
								{exportingPdf ? (
									<Loader2 className="h-3.5 w-3.5 animate-spin" />
								) : (
									<Download className="h-3.5 w-3.5" />
								)}
								导出 PDF
							</Button>
						)}
					</div>

					{showCommentExportOption && issueId && (
						<div className="flex items-center gap-2">
							<Checkbox
								id="include-comments-checkbox"
								checked={includeComments}
								onCheckedChange={(checked) => setIncludeComments(checked === true)}
							/>
							<label
								htmlFor="include-comments-checkbox"
								className="text-xs font-medium text-muted-foreground select-none cursor-pointer"
							>
								PDF 包含评论区内容
							</label>
						</div>
					)}
				</div>

				{/* Content Area */}
				<div className="min-h-0 flex-1 overflow-auto bg-background">
					<div ref={contentRef} className="px-6 py-4">
						<ReadonlyContent content={content || "*无内容*"} />
					</div>
				</div>
			</div>
		</div>,
		document.body
	);
}
