import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { ModalContainer } from "@buiducnhat/better-modal";
import { i18n } from "@lingui/core";
import { AppProvider } from "./bridge/app-provider";
import { BoardEditor } from "./board-editor";
import { BoardProvider } from "./board-context";
import { messages as en } from "./locales/en/messages.po";
import { messages as vi } from "./locales/vi/messages.po";
import "./index.css";

const catalogs = { en, vi };
// English until the host says otherwise, so the connecting state reads.
// eslint-disable-next-line lingui/no-unlocalized-strings -- locale tag
i18n.loadAndActivate({ locale: "en", messages: en });

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppProvider catalogs={catalogs}>
      <BoardProvider>
        <BoardEditor />
        {/* Inside the board's provider: the dialogs read the same live board. */}
        <ModalContainer />
      </BoardProvider>
    </AppProvider>
  </StrictMode>,
);
