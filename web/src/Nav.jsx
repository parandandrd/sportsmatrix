import React, { useEffect, useState } from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import Container from 'react-bootstrap/Container';
import Navbar from 'react-bootstrap/Navbar';
import Nav from 'react-bootstrap/Nav';
import NavDropDown from 'react-bootstrap/NavDropdown';
import { Link, useLocation } from 'react-router-dom';
import { GetVersion } from './util';
import { GroupBoards, useBoards } from './boards';
import './Dashboard.css';

// TopNav lists the boards this instance actually has, one entry per config
// section, in the config file's order: the ones showing on the panel first.
export default function TopNav() {
    const [version, setVersion] = useState('');
    const { boards } = useBoards();
    const location = useLocation();

    useEffect(() => {
        GetVersion((v) => setVersion(v));
    }, []);

    // the live board is meant to be looked at, not navigated from
    if (location.pathname === '/board') {
        return null;
    }

    const groups = GroupBoards(boards).filter((g) => g.main.kind);
    const item = (g) =>
        <NavDropDown.Item key={g.key} as={Link} to={`/b/${encodeURIComponent(g.main.name)}`}>
            {g.main.name}
        </NavDropDown.Item>;
    const on = groups.filter((g) => g.enabled);
    const off = groups.filter((g) => !g.enabled);

    return (
        <Container fluid>
            <Navbar expand="sm" bg="dark" variant="dark">
                <Navbar.Brand as={Link} to="/">SportsMatrix</Navbar.Brand>
                <Navbar.Toggle aria-controls="basic-navbar-nav" />
                <Navbar.Collapse id="basic-navbar-nav">
                    <Nav className="mr-auto">
                        <Nav.Link as={Link} to="/">Home</Nav.Link>
                        {groups.length > 0
                            ? <NavDropDown title="Boards" id="boards-drop">
                                {on.length > 0 ? <NavDropDown.Header>On the panel</NavDropDown.Header> : null}
                                {on.map(item)}
                                {on.length > 0 && off.length > 0 ? <NavDropDown.Divider /> : null}
                                {off.length > 0 ? <NavDropDown.Header>Off</NavDropDown.Header> : null}
                                {off.map(item)}
                            </NavDropDown>
                            : null}
                        <Nav.Link as={Link} to="/docs">API Docs</Nav.Link>
                        <Nav.Link as={Link} to="/board">Live Board</Nav.Link>
                    </Nav>
                    <Navbar.Text>{version}</Navbar.Text>
                </Navbar.Collapse>
            </Navbar>
        </Container>
    );
}
