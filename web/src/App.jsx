import { lazy, Suspense } from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import Board from './Board.jsx';
import BoardPage from './BoardPage.jsx';
import TopNav from './Nav.jsx';
import Dashboard from './Dashboard.jsx';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router';

const ApiDocs = lazy(() => import('./ApiDocs.jsx'));

// Routes used to name every league the binary can render, whether or not this
// instance ran it. The board pages are keyed on the name ListBoards reports
// instead, so adding a league means adding a league, not editing a route table.
export default function App() {
  return (
    <BrowserRouter>
      <TopNav />
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/b/:name" element={<BoardPage />} />
        <Route path="/board" element={<Board />} />
        <Route path="/docs" element={
          <Suspense fallback={<p className="dash-msg">Loading...</p>}><ApiDocs /></Suspense>
        } />
        {/* the per-league routes this used to define are gone; send their
            bookmarks home rather than rendering an empty page */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
