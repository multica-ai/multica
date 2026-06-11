"use client";

import { useState } from "react";
import {
	Sheet,
	SheetContent,
	SheetHeader,
	SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { Button } from "@multica/ui/components/ui/button";
import { Download, FileText, Loader2 } from "lucide-react";
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
		setExportingPdf(true);
		try {
			const blob = await api.exportIssue(issueId, {
				format: "pdf",
				include_comments: includeComments,
			});
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

	return (
		<Sheet open={open} onOpenChange={onOpenChange}>
			<SheetContent side="right" className="w-[90vw] sm:max-w-[700px] flex flex-col h-full p-0">
				<SheetHeader className="px-6 pt-6 pb-4 border-b">
					<SheetTitle className="text-lg font-semibold flex items-center gap-2">
						<FileText className="h-5 w-5 text-primary" />
						Markdown 预览：{title}
					</SheetTitle>
				</SheetHeader>

				{/* Toolbar */}
				<div className="px-6 py-3 bg-muted/40 border-b flex flex-wrap items-center justify-between gap-4">
					<div className="flex items-center gap-3">
						<Button
							variant="outline"
							size="sm"
							onClick={handleExportMD}
							className="flex items-center gap-1.5"
						>
							<Download className="h-4 w-4" />
							导出 MD
						</Button>

						{issueId && (
							<Button
								variant="default"
								size="sm"
								disabled={exportingPdf}
								onClick={handleExportPDF}
								className="flex items-center gap-1.5"
							>
								{exportingPdf ? (
									<Loader2 className="h-4 w-4 animate-spin" />
								) : (
									<Download className="h-4 w-4" />
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
				<div className="flex-1 overflow-y-auto px-6 py-6 bg-background dark:bg-card">
					<div className="prose prose-sm dark:prose-invert max-w-none">
						<ReadonlyContent content={content || "*无内容*"} />
					</div>
				</div>
			</SheetContent>
		</Sheet>
	);
}
