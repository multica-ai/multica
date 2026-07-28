/**
 * Live co-editing for a note (FIR-1317) — the client half.
 *
 * Connects the shared ContentEditor to the server's prosemirror-collab
 * authority: local steps go up, everyone's steps come down, carets and
 * presence are relayed. The editor component itself is untouched; the collab
 * and remote-caret plugins are registered on the live editor instance.
 *
 * Saving is deliberately unchanged. The note body is still written through the
 * normal autosave, so version history, Line history and search keep working.
 */

import * as React from "react";
import type { Editor } from "@tiptap/react";
import {
  collab,
  getVersion,
  receiveTransaction,
  sendableSteps,
} from "prosemirror-collab";
import { Step } from "@tiptap/pm/transform";

import { api } from "@multica/core/api";

import {
  liveCollabUrl,
  otherPeers,
  pruneCarets,
  upsertCaret,
  type LivePeer,
  type RemoteCaret,
  type ServerMessage,
} from "./note-live-collab-protocol";
import {
  remoteCaretsKey,
  remoteCaretsPlugin,
} from "./note-remote-carets-plugin";

const CARET_THROTTLE_MS = 120;
const RECONNECT_DELAY_MS = 2000;

export interface NoteLiveCollabState {
  /** True once the room is joined and the editor is wired to it. */
  connected: boolean;
  /** Everyone else currently in this note. */
  peers: LivePeer[];
}

export interface UseNoteLiveCollabOptions {
  editor: Editor | null;
  noteId: string;
  enabled: boolean;
}

/**
 * useNoteLiveCollab joins the note's live room and keeps the editor in sync.
 * It is a no-op when the feature flag is off or the editor is not mounted yet,
 * so the note behaves exactly as before.
 */
export function useNoteLiveCollab({
  editor,
  noteId,
  enabled,
}: UseNoteLiveCollabOptions): NoteLiveCollabState {
  const [connected, setConnected] = React.useState(false);
  const [peers, setPeers] = React.useState<LivePeer[]>([]);

  const socketRef = React.useRef<WebSocket | null>(null);
  const peerIdRef = React.useRef<string>("");
  const caretsRef = React.useRef<RemoteCaret[]>([]);
  const lastCaretSentRef = React.useRef(0);
  const pluginsRegisteredRef = React.useRef(false);

  // Repaint remote carets by pushing them onto the editor as plugin metadata.
  const paintCarets = React.useCallback(
    (target: Editor) => {
      const tr = target.state.tr;
      tr.setMeta(remoteCaretsKey, caretsRef.current);
      tr.setMeta("addToHistory", false);
      target.view.dispatch(tr);
    },
    [],
  );

  const send = React.useCallback((payload: unknown) => {
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify(payload));
  }, []);

  // Push any pending local steps to the authority. prosemirror-collab keeps
  // exactly one batch in flight; the next batch goes out when this one is
  // confirmed back to us.
  const flushSteps = React.useCallback(
    (target: Editor) => {
      const sendable = sendableSteps(target.state);
      if (!sendable) return;
      send({
        type: "steps",
        version: sendable.version,
        steps: sendable.steps.map((s: Step) => s.toJSON()),
      });
    },
    [send],
  );

  React.useEffect(() => {
    if (!enabled || !editor || !noteId) return;

    let disposed = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined;

    const applyRemoteSteps = (
      incoming: { client_id: string; step: unknown }[],
    ) => {
      if (incoming.length === 0) return;
      const { state } = editor;
      const steps = incoming.map((s) =>
        Step.fromJSON(state.schema, s.step as Parameters<typeof Step.fromJSON>[1]),
      );
      const clientIDs = incoming.map((s) => s.client_id);
      editor.view.dispatch(receiveTransaction(state, steps, clientIDs));
      // Our own queued steps may now be sendable on the new version.
      flushSteps(editor);
    };

    const handleMessage = (raw: MessageEvent<string>) => {
      let msg: ServerMessage;
      try {
        msg = JSON.parse(raw.data) as ServerMessage;
      } catch {
        return;
      }

      switch (msg.type) {
        case "welcome": {
          peerIdRef.current = msg.peer_id;
          // A joiner adopts the room's live document before wiring collab, so
          // it starts from what the others actually see rather than from the
          // saved body it loaded a moment ago.
          if (msg.snapshot?.doc) {
            editor.commands.setContent(
              msg.snapshot.doc as Parameters<
                typeof editor.commands.setContent
              >[0],
              { emitUpdate: false },
            );
          }
          if (!pluginsRegisteredRef.current) {
            editor.registerPlugin(
              collab({ version: msg.version, clientID: msg.peer_id }),
            );
            editor.registerPlugin(remoteCaretsPlugin());
            pluginsRegisteredRef.current = true;
          }
          setPeers(otherPeers(msg.peers, msg.peer_id));
          setConnected(true);
          flushSteps(editor);
          break;
        }

        case "peers": {
          const others = otherPeers(msg.peers, peerIdRef.current);
          setPeers(others);
          caretsRef.current = pruneCarets(caretsRef.current, msg.peers);
          paintCarets(editor);
          break;
        }

        case "steps_applied":
          applyRemoteSteps(msg.steps);
          break;

        case "rejected":
          // Someone else got there first: take their steps, rebase, resend.
          applyRemoteSteps(msg.missing);
          break;

        case "caret": {
          if (msg.peer_id === peerIdRef.current) break;
          caretsRef.current = upsertCaret(caretsRef.current, {
            peerId: msg.peer_id,
            name: msg.name,
            color: msg.color,
            anchor: msg.anchor,
            head: msg.head,
          });
          paintCarets(editor);
          break;
        }

        case "snapshot_request":
          send({
            type: "snapshot",
            version: getVersion(editor.state),
            doc: editor.getJSON(),
          });
          break;

        case "reload":
          // We fell too far behind to replay; the saved body is authoritative.
          setConnected(false);
          socketRef.current?.close();
          break;

        default:
          break;
      }
    };

    const connect = () => {
      if (disposed) return;
      const socket = new WebSocket(liveCollabUrl(api.getBaseUrl(), noteId));
      socketRef.current = socket;
      socket.onmessage = handleMessage;
      socket.onclose = () => {
        if (disposed) return;
        setConnected(false);
        setPeers([]);
        reconnectTimer = setTimeout(connect, RECONNECT_DELAY_MS);
      };
    };

    const onTransaction = () => flushSteps(editor);
    const onSelection = () => {
      const now = Date.now();
      if (now - lastCaretSentRef.current < CARET_THROTTLE_MS) return;
      lastCaretSentRef.current = now;
      const { from, to } = editor.state.selection;
      send({ type: "caret", anchor: from, head: to });
    };

    editor.on("transaction", onTransaction);
    editor.on("selectionUpdate", onSelection);
    connect();

    return () => {
      disposed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      editor.off("transaction", onTransaction);
      editor.off("selectionUpdate", onSelection);
      socketRef.current?.close();
      socketRef.current = null;
      setConnected(false);
      setPeers([]);
    };
  }, [editor, noteId, enabled, flushSteps, paintCarets, send]);

  return { connected, peers };
}
