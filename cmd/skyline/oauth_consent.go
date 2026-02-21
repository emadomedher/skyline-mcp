package main

import "fmt"

func renderConsentPage(clientName, clientID, redirectURI, codeChallenge, codeChallengeMethod, state string) string {
	return renderConsentPageWithError(clientName, clientID, redirectURI, codeChallenge, codeChallengeMethod, state, "")
}

func renderConsentPageWithError(clientName, clientID, redirectURI, codeChallenge, codeChallengeMethod, state, errMsg string) string {
	errorHTML := ""
	if errMsg != "" {
		errorHTML = fmt.Sprintf(`<div style="background:#ff4444;color:#fff;padding:10px 16px;border-radius:8px;margin-bottom:20px;font-size:14px;">%s</div>`, errMsg)
	}
	displayName := clientName
	if displayName == "" {
		displayName = clientID
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Skyline - Authorize Application</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #0f0f23;
    color: #e0e0e0;
    display: flex;
    justify-content: center;
    align-items: center;
    min-height: 100vh;
    padding: 20px;
  }
  .card {
    background: #1a1a2e;
    border: 1px solid #2a2a4a;
    border-radius: 16px;
    padding: 40px;
    max-width: 420px;
    width: 100%%;
    box-shadow: 0 8px 32px rgba(0,0,0,0.4);
  }
  .logo {
    text-align: center;
    margin-bottom: 24px;
  }
  .logo h1 {
    font-size: 24px;
    color: #7c6aef;
    font-weight: 700;
  }
  .logo p {
    color: #888;
    font-size: 13px;
    margin-top: 4px;
  }
  .prompt {
    text-align: center;
    margin-bottom: 28px;
    padding: 16px;
    background: #16162b;
    border-radius: 10px;
    border: 1px solid #2a2a4a;
  }
  .prompt .client-name {
    font-weight: 600;
    color: #a78bfa;
    font-size: 16px;
  }
  .prompt .desc {
    color: #999;
    font-size: 13px;
    margin-top: 6px;
  }
  label {
    display: block;
    font-size: 13px;
    color: #aaa;
    margin-bottom: 6px;
    font-weight: 500;
  }
  input[type="text"], input[type="password"] {
    width: 100%%;
    padding: 10px 14px;
    background: #0f0f23;
    border: 1px solid #333;
    border-radius: 8px;
    color: #e0e0e0;
    font-size: 14px;
    margin-bottom: 16px;
    outline: none;
    transition: border-color 0.2s;
  }
  input:focus { border-color: #7c6aef; }
  .actions {
    display: flex;
    gap: 12px;
    margin-top: 8px;
  }
  button {
    flex: 1;
    padding: 12px;
    border: none;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
    transition: opacity 0.2s;
  }
  button:hover { opacity: 0.85; }
  .btn-authorize {
    background: #7c6aef;
    color: #fff;
  }
  .btn-deny {
    background: #333;
    color: #ccc;
  }
  .note {
    text-align: center;
    font-size: 11px;
    color: #666;
    margin-top: 20px;
  }
</style>
</head>
<body>
<div class="card">
  <div class="logo">
    <h1>Skyline MCP</h1>
    <p>Authorization Request</p>
  </div>
  %s
  <div class="prompt">
    <div class="client-name">%s</div>
    <div class="desc">wants to access your Skyline MCP tools</div>
  </div>
  <form method="POST" action="/oauth/authorize">
    <input type="hidden" name="client_id" value="%s">
    <input type="hidden" name="redirect_uri" value="%s">
    <input type="hidden" name="code_challenge" value="%s">
    <input type="hidden" name="code_challenge_method" value="%s">
    <input type="hidden" name="state" value="%s">
    <label for="profile_name">Profile Name</label>
    <input type="text" id="profile_name" name="profile_name" placeholder="e.g. production" required autocomplete="off">
    <label for="profile_token">Profile Token</label>
    <input type="password" id="profile_token" name="profile_token" placeholder="Your profile bearer token" required>
    <div class="actions">
      <button type="submit" name="action" value="deny" class="btn-deny">Deny</button>
      <button type="submit" name="action" value="authorize" class="btn-authorize">Authorize</button>
    </div>
  </form>
  <p class="note">This grants access to the MCP tools configured in your profile.</p>
</div>
</body>
</html>`, errorHTML, displayName, clientID, redirectURI, codeChallenge, codeChallengeMethod, state)
}
