import React from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import Sport from './Sport.js';
import Racing from './Racing.js';
import ImageBoard from './ImageBoard.js';
import Board from './Board.js';
import TopNav from './Nav.js';
import All from './All.js';
import BasicBoard from './BasicBoard';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import SwaggerUI from 'swagger-ui-react';
import "swagger-ui-react/swagger-ui.css";
import swag from './matrix.swagger.json';

class App extends React.Component {
  render() {
    return (
      <>
        <BrowserRouter>
          <TopNav />
          <Routes>
            <Route path="/" element={<All />} />
            <Route path="/mlb" element={<Sport sport="mlb" id="mlb" key="mlb" withImg="true" />} />
            <Route path="/ncaaf" element={<Sport sport="ncaaf" id="ncaaf" key="ncaaf" withImg="true" />} />
            <Route path="/nhl" element={<Sport sport="nhl" id="nhl" key="nhl" withImg="true" />} />
            <Route path="/ncaam" element={<Sport sport="ncaam" id="ncaam" key="ncaam" withImg="true" />} />
            <Route path="/nfl" element={<Sport sport="nfl" id="nfl" key="nfl" withImg="true" />} />
            <Route path="/nba" element={<Sport sport="nba" id="nba" key="nba" withImg="true" />} />
            <Route path="/mls" element={<Sport sport="mls" id="mls" key="mls" withImg="true" />} />
            <Route path="/epl" element={<Sport sport="epl" id="epl" key="epl" withImg="true" />} />
            <Route path="/dfl" element={<Sport sport="dfl" id="dfl" key="dfl" withImg="true" />} />
            <Route path="/dfb" element={<Sport sport="dfb" id="dfb" key="dfb" withImg="true" />} />
            <Route path="/uefa" element={<Sport sport="uefa" id="uefa" key="uefa" withImg="true" />} />
            <Route path="/fifa" element={<Sport sport="fifa" id="fifa" key="fifa" withImg="true" />} />
            <Route path="/pga" element={<BasicBoard id="pga" name="pga" key="pga" path="stat/pga" withImg="true" />} />
            <Route path="/img" element={<ImageBoard withImg="true" />} />
            <Route path="/clock" element={<BasicBoard id="clock" name="clock" key="clock" withImg="true" />} />
            <Route path="/sys" element={<BasicBoard id="sys" name="sys" key="sys" withImg="true" />} />
            <Route path="/gcal" element={<BasicBoard id="gcal" name="gcal" key="gcal" withImg="true" />} />
            <Route path="/board" element={<Board />} />
            <Route path="/docs" element={<SwaggerUI spec={swag} />} />
            <Route path="/f1" element={<Racing sport="f1" id="f1" key="f1" withImg="true" />} />
            <Route path="/irl" element={<Racing sport="irl" id="irl" key="irl" withImg="true" />} />
            <Route path="/ncaaw" element={<Sport sport="ncaaw" id="ncaaw" key="ncaaw" withImg="true" />} />
            <Route path="/wnba" element={<Sport sport="wnba" id="wnba" key="wnba" withImg="true" />} />
            <Route path="/ligue" element={<Sport sport="ligue" id="ligue" key="ligue" withImg="true" />} />
            <Route path="/seriea" element={<Sport sport="seriea" id="seriea" key="seriea" withImg="true" />} />
            <Route path="/laliga" element={<Sport sport="laliga" id="laliga" key="laliga" withImg="true" />} />
            <Route path="/xfl" element={<Sport sport="xfl" id="xfl" key="xfl" withImg="true" />} />
            <Route path="/nwsl" element={<Sport sport="nwsl" id="nwsl" key="nwsl" withImg="true" />} />
          </Routes>
        </BrowserRouter>
        <hr />
      </>
    );
  }
}
export default App;