import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { MDXProvider } from "@mdx-js/react";

import "./docs.css";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root");

// MDX-rendered components inherit no styling by default. We pass a
// minimal component map so headings, paragraphs, lists, code blocks,
// and tables get docs-prose class names that we style in docs.css.
createRoot(root).render(
  <StrictMode>
    <BrowserRouter basename={import.meta.env.BASE_URL}>
      <MDXProvider>
        <App />
      </MDXProvider>
    </BrowserRouter>
  </StrictMode>,
);
