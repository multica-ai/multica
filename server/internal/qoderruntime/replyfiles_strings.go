package qoderruntime

import "strings"

// Reasons remain readable English in the journal and logs. Localize them only
// when rendering the reply, including reasons saved by an earlier bridge.
type replyFileCopy struct {
	title   string
	reasons map[string]string
}

func (c replyFileCopy) reason(reason string) string {
	if text, ok := c.reasons[reason]; ok {
		return text
	}
	return reason + "."
}

func replyFileCopyFor(locale string) replyFileCopy {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "zh-hant" || strings.HasPrefix(locale, "zh-hant-") || locale == "zh-tw" || locale == "zh-hk" {
		return replyFileCopies["zh-Hant"]
	}
	language, _, _ := strings.Cut(locale, "-")
	if language == "zh" {
		return replyFileCopies["zh-Hans"]
	}
	if copy, ok := replyFileCopies[language]; ok {
		return copy
	}
	return replyFileCopy{title: "Attachment delivery notice:"}
}

var replyFileCopies = map[string]replyFileCopy{
	"zh-Hans": {
		title: "附件发送提示：",
		reasons: map[string]string{
			"not attached because the bridge restarted during the run":               "运行服务已重启，文件未附上。",
			"not attached because it exceeds the 10 MB limit":                        "文件超过 10 MB，未附上。",
			"not attached because Qoder did not offer the file for download":         "Qoder 未提供可下载文件，未附上。",
			"not attached because access to the file could not be verified":          "无法确认文件访问权限，未附上。",
			"not attached because the download was refused":                          "文件下载被拒绝，未附上。",
			"not attached because Multica refused the upload":                        "Multica 拒绝了文件上传，未附上。",
			"delivery could not be confirmed; check the attachments before retrying": "无法确认文件是否已附上，请先检查附件再决定是否重试。",
			"not attached because Multica could not save the attachment":             "Multica 未能保存附件，文件未附上。",
			"not attached because the file transfer failed after 3 attempts":         "文件传输尝试 3 次仍未成功，未附上。",
		},
	},
	"zh-Hant": {
		title: "附件傳送提示：",
		reasons: map[string]string{
			"not attached because the bridge restarted during the run":               "執行服務已重新啟動，未附上檔案。",
			"not attached because it exceeds the 10 MB limit":                        "檔案超過 10 MB，未附上。",
			"not attached because Qoder did not offer the file for download":         "Qoder 未提供可下載的檔案，未附上。",
			"not attached because access to the file could not be verified":          "無法確認檔案存取權限，未附上。",
			"not attached because the download was refused":                          "檔案下載遭拒，未附上。",
			"not attached because Multica refused the upload":                        "Multica 拒絕了檔案上傳，未附上。",
			"delivery could not be confirmed; check the attachments before retrying": "無法確認是否已附上檔案，請先檢查附件再決定是否重試。",
			"not attached because Multica could not save the attachment":             "Multica 未能儲存附件，未附上檔案。",
			"not attached because the file transfer failed after 3 attempts":         "檔案傳輸嘗試 3 次仍未成功，未附上。",
		},
	},
	"ja": {
		title: "添付ファイルの送信状況：",
		reasons: map[string]string{
			"not attached because the bridge restarted during the run":               "実行サービスが再起動したため、添付されませんでした。",
			"not attached because it exceeds the 10 MB limit":                        "10 MB を超えているため、添付されませんでした。",
			"not attached because Qoder did not offer the file for download":         "Qoder からダウンロードできないため、添付されませんでした。",
			"not attached because access to the file could not be verified":          "ファイルへのアクセス権を確認できなかったため、添付されませんでした。",
			"not attached because the download was refused":                          "ダウンロードが拒否されたため、添付されませんでした。",
			"not attached because Multica refused the upload":                        "Multica がアップロードを拒否したため、添付されませんでした。",
			"delivery could not be confirmed; check the attachments before retrying": "添付されたか確認できません。再試行する前に添付ファイルを確認してください。",
			"not attached because Multica could not save the attachment":             "Multica が添付ファイルを保存できませんでした。",
			"not attached because the file transfer failed after 3 attempts":         "3 回試行しても転送できなかったため、添付されませんでした。",
		},
	},
	"ko": {
		title: "첨부 파일 전송 안내:",
		reasons: map[string]string{
			"not attached because the bridge restarted during the run":               "실행 서비스가 다시 시작되어 파일이 첨부되지 않았습니다.",
			"not attached because it exceeds the 10 MB limit":                        "파일이 10 MB를 초과하여 첨부되지 않았습니다.",
			"not attached because Qoder did not offer the file for download":         "Qoder에서 파일을 다운로드할 수 없어 첨부되지 않았습니다.",
			"not attached because access to the file could not be verified":          "파일 접근 권한을 확인할 수 없어 첨부되지 않았습니다.",
			"not attached because the download was refused":                          "다운로드가 거부되어 파일이 첨부되지 않았습니다.",
			"not attached because Multica refused the upload":                        "Multica에서 업로드를 거부하여 파일이 첨부되지 않았습니다.",
			"delivery could not be confirmed; check the attachments before retrying": "파일 첨부 여부를 확인할 수 없습니다. 다시 시도하기 전에 첨부 파일을 확인해 주세요.",
			"not attached because Multica could not save the attachment":             "Multica에서 첨부 파일을 저장하지 못했습니다.",
			"not attached because the file transfer failed after 3 attempts":         "3번 시도 후에도 파일을 전송하지 못해 첨부되지 않았습니다.",
		},
	},
	"fr": {
		title: "État de l’envoi des pièces jointes :",
		reasons: map[string]string{
			"not attached because the bridge restarted during the run":               "Le service d’exécution a redémarré ; le fichier n’a pas été joint.",
			"not attached because it exceeds the 10 MB limit":                        "Le fichier dépasse 10 Mo et n’a pas été joint.",
			"not attached because Qoder did not offer the file for download":         "Qoder ne permet pas de télécharger ce fichier ; il n’a pas été joint.",
			"not attached because access to the file could not be verified":          "L’accès au fichier n’a pas pu être vérifié ; il n’a pas été joint.",
			"not attached because the download was refused":                          "Le téléchargement a été refusé ; le fichier n’a pas été joint.",
			"not attached because Multica refused the upload":                        "Multica a refusé le téléversement ; le fichier n’a pas été joint.",
			"delivery could not be confirmed; check the attachments before retrying": "L’envoi n’a pas pu être confirmé. Vérifiez les pièces jointes avant de réessayer.",
			"not attached because Multica could not save the attachment":             "Multica n’a pas pu enregistrer la pièce jointe.",
			"not attached because the file transfer failed after 3 attempts":         "Le transfert a échoué après 3 tentatives ; le fichier n’a pas été joint.",
		},
	},
}
