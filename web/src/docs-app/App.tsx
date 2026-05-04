import { Route, Routes } from "react-router-dom";

import { DocsLayout } from "./DocsLayout";
import { pages } from "./pages";

export function App() {
  return (
    <DocsLayout>
      <Routes>
        {pages.map((p) => (
          <Route
            key={p.path}
            path={p.path}
            element={<p.Component />}
          />
        ))}
        <Route path="*" element={<NotFound />} />
      </Routes>
    </DocsLayout>
  );
}

function NotFound() {
  return (
    <div>
      <h1>Not found</h1>
      <p>
        The page you were looking for doesn&apos;t exist on this site. Try
        the sidebar.
      </p>
    </div>
  );
}
