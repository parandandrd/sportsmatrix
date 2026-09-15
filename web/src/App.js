import React from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import Board from './Board.js';
import BoardPage from './BoardPage.js';
import TopNav from './Nav.js';
import Dashboard from './Dashboard.js';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import SwaggerUI from 'swagger-ui-react';
import "swagger-ui-react/swagger-ui.css";
import swag from './matrix.swagger.json';

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
        <Route path="/docs" element={<SwaggerUI spec={swag} />} />
        {/* the per-league routes this used to define are gone; send their
            bookmarks home rather than rendering an empty page */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
