import type { MessageBoxOptions } from "electron";

const copy = {
 en: ["Switch local execution service", "Another account owns this backend's local execution service.", "The local execution service was started outside this Desktop profile.", "Waiting stops new tasks and lets active tasks finish. Interrupting cancels active tasks. The replacement starts only after shutdown and cleanup are confirmed.", "Cancel", "Wait and switch", "Interrupt and switch"],
 "zh-Hans": ["切换本地执行服务", "此后端的本地执行服务属于另一个账号。", "本地执行服务由当前桌面配置之外的入口启动。", "等待会停止接收新任务并让当前任务完成。立即中断会取消当前任务。只有确认停止和清理完成后才会启动替代进程。", "取消", "等待完成后切换", "立即中断并切换"],
 "zh-Hant": ["切換本機執行服務", "此後端的本機執行服務屬於另一個帳號。", "本機執行服務由目前桌面設定以外的入口啟動。", "等待會停止接收新任務並讓目前任務完成。立即中斷會取消目前任務。只有確認停止與清理完成後才啟動替代程序。", "取消", "等待完成後切換", "立即中斷並切換"],
 ja: ["ローカル実行サービスを切り替え", "このバックエンドの実行サービスは別のアカウントが所有しています。", "実行サービスは現在のデスクトッププロファイル以外から起動されています。", "待機中は新しいタスクを受け付けません。中断すると実行中のタスクがキャンセルされます。停止とクリーンアップの確認後にのみ再起動します。", "キャンセル", "完了を待って切り替え", "中断して切り替え"],
 ko: ["로컬 실행 서비스 전환", "이 백엔드의 실행 서비스는 다른 계정에 속합니다.", "실행 서비스가 현재 데스크톱 프로필 외부에서 시작되었습니다.", "대기 중에는 새 작업을 받지 않습니다. 중단하면 실행 중인 작업이 취소됩니다. 종료와 정리를 확인한 후에만 새 프로세스를 시작합니다.", "취소", "완료 후 전환", "중단하고 전환"],
 fr: ["Changer le service local", "Le service de ce serveur appartient à un autre compte.", "Le service a été lancé hors de ce profil Desktop.", "Attendre suspend les nouvelles tâches. Interrompre annule les tâches actives. Le remplacement démarre uniquement après confirmation de l’arrêt et du nettoyage.", "Annuler", "Attendre et changer", "Interrompre et changer"],
 es: ["Cambiar el servicio local", "El servicio de este servidor pertenece a otra cuenta.", "El servicio se inició fuera de este perfil de Desktop.", "Esperar suspende las tareas nuevas. Interrumpir cancela las tareas activas. El reemplazo solo comienza tras confirmar la detención y la limpieza.", "Cancelar", "Esperar y cambiar", "Interrumpir y cambiar"],
} as const;

export function handoffConfirmationOptions(language: string, differentAccount: boolean): MessageBoxOptions {
 const key = language.startsWith("zh") ? (/TW|HK|Hant/i.test(language) ? "zh-Hant" : "zh-Hans") : language.split("-")[0];
 const text = copy[key as keyof typeof copy] ?? copy.en;
 return { type:"warning", title:text[0], message:text[differentAccount ? 1 : 2], detail:text[3], buttons:[text[4],text[5],text[6]], defaultId:0, cancelId:0, noLink:true };
}

const stopCopy = {
 en:["Stop shared execution service", "This service is also used outside Desktop. Stopping it affects tasks started from other entry points.", "Wait and stop", "Interrupt and stop"],
 "zh-Hans":["停止共享执行服务", "此服务也由桌面端之外的入口使用。停止会影响其他入口发起的任务。", "等待完成后停止", "立即中断并停止"],
 "zh-Hant":["停止共用執行服務", "此服務也由桌面端以外的入口使用。停止會影響其他入口發起的任務。", "等待完成後停止", "立即中斷並停止"],
 ja:["共有実行サービスを停止", "このサービスはデスクトップ以外からも使用されています。停止すると他の入口から開始したタスクにも影響します。", "完了を待って停止", "中断して停止"],
 ko:["공유 실행 서비스 중지", "이 서비스는 데스크톱 외부에서도 사용됩니다. 중지하면 다른 경로에서 시작한 작업에도 영향을 줍니다.", "완료 후 중지", "중단하고 중지"],
 fr:["Arrêter le service partagé", "Ce service est aussi utilisé hors de Desktop. Son arrêt affecte les tâches lancées ailleurs.", "Attendre et arrêter", "Interrompre et arrêter"],
 es:["Detener el servicio compartido", "Este servicio también se usa fuera de Desktop. Detenerlo afecta a las tareas iniciadas desde otras entradas.", "Esperar y detener", "Interrumpir y detener"],
} as const;

export function sharedStopConfirmationOptions(language:string): MessageBoxOptions {
 const key=language.startsWith("zh") ? (/TW|HK|Hant/i.test(language) ? "zh-Hant" : "zh-Hans") : language.split("-")[0];
 const text=stopCopy[key as keyof typeof stopCopy] ?? stopCopy.en;
 const base=handoffConfirmationOptions(language,false);
 return {...base,title:text[0],message:text[1],buttons:[base.buttons![0]!,text[2],text[3]]};
}
