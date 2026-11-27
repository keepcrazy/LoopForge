import test from 'node:test';
import assert from 'node:assert/strict';
import { loadApp, settle } from '../lib/load-app.mjs';

const localUser = {
  id: 1, loginName: 'alice', chineseName: 'Alice', displayName: 'Alice W',
  passwordLogin: true, avatarConfigured: true, avatarVersion: 7, isGuest: false,
};

async function render(app, user) {
  app.evaluate(`state.currentUser=${JSON.stringify(user)};state.data.users=[${JSON.stringify(user)}];renderPersonalSettings();`);
  await settle(4);
}

test('personal profile edits display name, changes password, and restores the default avatar', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  app.router.put('/api/users/1/profile', (call) => ({
    id: 1, loginName: 'alice', chineseName: 'Alice', displayName: call.body.displayName,
    passwordLogin: true, avatarConfigured: true, avatarVersion: 7,
  }));
  app.router.put('/api/auth/password', { ok: true });
  app.router.delete('/api/users/1/avatar', {
    user: { id: 1, loginName: 'alice', chineseName: 'Alice', passwordLogin: true, avatarConfigured: false },
    avatarConfigured: false,
    avatarVersion: 0,
  });
  await render(app, localUser);

  const card = () => app.document.querySelector('#personalProfileCard');
  assert.ok(card(), 'personal settings shows the profile card');
  assert.equal(app.document.querySelector('#profileNameForm').elements.displayName.value, 'Alice W');
  assert.match(card().textContent, /本地账号/);
  assert.ok(app.document.querySelector('#profilePasswordForm'), 'password accounts can change their password');
  assert.ok(app.document.querySelector('#profileAvatarRemove'), 'uploaded avatars can be removed');

  // Display name save updates the card without rebuilding the credential panel.
  const nameForm = app.document.querySelector('#profileNameForm');
  nameForm.elements.displayName.value = 'Alice Wong';
  nameForm.dispatchEvent(new app.window.Event('submit', { bubbles: true, cancelable: true }));
  await settle(6);
  const saved = app.router.calls.find((call) => call.path === '/api/users/1/profile');
  assert.deepEqual(saved.body, { displayName: 'Alice Wong' });
  assert.equal(app.document.querySelector('#profileNameForm').elements.displayName.value, 'Alice Wong');

  // Password change sends the current and new password.
  const passwordForm = app.document.querySelector('#profilePasswordForm');
  passwordForm.elements.currentPassword.value = 'old-password';
  passwordForm.elements.newPassword.value = 'new-password';
  passwordForm.elements.confirmPassword.value = 'new-password';
  passwordForm.dispatchEvent(new app.window.Event('submit', { bubbles: true, cancelable: true }));
  await settle(6);
  const changed = app.router.calls.find((call) => call.path === '/api/auth/password');
  assert.deepEqual(changed.body, { currentPassword: 'old-password', newPassword: 'new-password' });
  assert.equal(passwordForm.elements.newPassword.value, '', 'successful change clears the form');

  // Restoring the system avatar removes the uploaded one.
  app.document.querySelector('#profileAvatarRemove').click();
  await settle(6);
  const removed = app.router.calls.find((call) => call.path === '/api/users/1/avatar' && call.method === 'DELETE');
  assert.ok(removed);
  assert.equal(app.evaluate('state.currentUser.avatarConfigured'), false);
  assert.equal(app.document.querySelector('#profileAvatarRemove'), null);
});

test('single sign-on accounts do not get a platform password form', async (t) => {
  const app = await loadApp();
  t.after(app.close);
  await render(app, { id: 1, loginName: 'bob', chineseName: 'Bob', isGuest: false, passwordLogin: false, avatarConfigured: false });

  assert.equal(app.document.querySelector('#profilePasswordForm'), null);
  assert.match(app.document.querySelector('#profileSecurityCard').textContent, /密码由身份系统管理/);
  assert.equal(app.document.querySelector('#profileAvatarRemove'), null);
  assert.match(app.document.querySelector('#personalProfileCard').textContent, /当前使用系统头像/);
});
