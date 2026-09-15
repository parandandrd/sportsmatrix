import React, { useEffect, useState } from 'react';
import 'bootstrap/dist/css/bootstrap.min.css';
import Container from 'react-bootstrap/Container';
import Navbar from 'react-bootstrap/Navbar';
import Nav from 'react-bootstrap/Nav';
import NavDropDown from 'react-bootstrap/NavDropdown';
import { Link, useLocation } from 'react-router-dom';
import { GetVersion, ListBoards } from './util';
import './Dashboard.css';

// TopNav lists the boards this instance actually has. It used to hard-code all
// 22 leagues the binary knows, including two -- stocks and weather -- that only
// exist in the premium build and had no route at all here.
export default function TopNav() {
    const [version, setVersion] = useState('');
    const [boards, setBoards] = useState([]);
    const location = useLocation();

    useEffect(() => {
        GetVersion((v) => setVersion(v));
        ListBoards()
            .then((b) => setBoards(b))
            .catch((err) => console.log('nav could not list boards', err));
    }, []);

    // the live board is meant to be looked at, not navigated from
    if (location.pathname === '/board') {
        return null;
    }

    return (
        <Container fluid>
            <Navbar expand="sm" bg="dark" variant="dark">
                <Navbar.Brand as={Link} to="/">SportsMatrix</Navbar.Brand>
                <Navbar.Toggle aria-controls="basic-navbar-nav" />
                <Navbar.Collapse id="basic-navbar-nav">
                    <Nav className="mr-auto">
                        <Nav.Link as={Link} to="/">Home</Nav.Link>
                        {boards.length > 0
                            ? <NavDropDown title="Boards" id="boards-drop">
                                {boards.filter((b) => b.kind).map((b) =>
                                    <NavDropDown.Item
                                        key={b.name}
                                        as={Link}
                                        to={`/b/${encodeURIComponent(b.name)}`}
                                    >
                                        {b.name}
                                    </NavDropDown.Item>)}
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
