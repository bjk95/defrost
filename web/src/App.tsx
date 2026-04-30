import { Routes, Route } from "react-router-dom";
import { Grid } from "@/components/Grid";
import { RunDetailSheet } from "@/components/RunDetailSheet";

export default function App() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b px-6 py-4">
        <h1 className="text-lg font-semibold">defrost</h1>
        <p className="text-sm text-muted-foreground">test history</p>
      </header>
      <main className="p-6">
        <Routes>
          <Route path="/" element={<Grid />} />
        </Routes>
        <RunDetailSheet />
      </main>
    </div>
  );
}
