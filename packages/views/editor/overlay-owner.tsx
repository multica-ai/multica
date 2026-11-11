"use client";

import { createContext, useContext } from "react";

/** Portaled editor controls remain part of the surface that owns the editor. */
export const EditorOverlayOwnerContext = createContext<string | undefined>(undefined);
export const useEditorOverlayOwner = () => useContext(EditorOverlayOwnerContext);
