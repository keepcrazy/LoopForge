const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const bundle = fs.readFileSync(process.argv[2], 'utf8');
function source(name) {
  const match = new RegExp('^(?:async )?function ' + name + '\\(', 'm').exec(bundle);
  assert(match, name);
  const tail = bundle.slice(match.index);
  const next = /\n(?:async )?function |\n(?:let|const) [A-Za-z]/.exec(tail);
  return next ? tail.slice(0, next.index) : tail;
}
const context = {};
vm.createContext(context);
for (const name of ['userDisplayName', 'userDisplay', 'userAvatarURL', 'personalProfileHTML']) vm.runInContext(source(name), context);
context.escapeHTML = value => String(value ?? '').replace(/[&<>'"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));
context.userAvatarHTML = () => '<img class="user-avatar-img" src="avatar.png" alt="">';
context.userById = id => id === 7 ? {id: 7, avatarVersion: 42} : null;

const named = {id: 7, loginName: 'alice', chineseName: '爱丽丝', displayName: 'Alice W'};
assert.equal(context.userDisplayName(named), 'Alice W');
assert.equal(context.userDisplayName({id: 8, loginName: 'bob', chineseName: '鲍勃'}), '鲍勃');
assert.equal(context.userDisplay({id: 7, loginName: 'alice', displayName: 'Alice W'}), 'alice(Alice W)');

assert.equal(context.userAvatarURL({id: 7, loginName: 'alice', avatarVersion: 42}, 48), '/api/auth/photo/48/alice.png?v=42');
assert.equal(context.userAvatarURL({id: 8, loginName: 'a/b c'}, 100), '/api/auth/photo/100/a%2Fb%20c.png');
// A lightweight user object (for example an issue assignee) resolves the
// version from the directory entry.
assert.equal(context.userAvatarURL({id: 7, loginName: 'alice'}, 100), '/api/auth/photo/100/alice.png?v=42');
assert.equal(context.userAvatarURL({id: 9, loginName: 'carol'}, 100), '/api/auth/photo/100/carol.png');
const profile = context.personalProfileHTML({id: 7, loginName: 'alice', chineseName: '爱丽丝', displayName: 'Alice W', deptName: '平台组', avatarVersion: 42, passwordLogin: true}, {avatarConfigured: true, passwordLogin: true});
assert.ok(profile.includes('id="personalProfileCard"'));
assert.ok(profile.includes('id="profileAvatarUpload"'));
assert.ok(profile.includes('id="profileAvatarRemove"'), 'a custom avatar can be removed');
assert.ok(profile.includes('id="profileNameForm"') && profile.includes('value="Alice W"'));
assert.ok(profile.includes('id="profilePasswordForm"') && profile.includes('当前密码') && profile.includes('确认新密码'));
assert.ok(profile.includes('认证方式') && profile.includes('本地账号'));
assert.ok(profile.includes('平台组'));

const sso = context.personalProfileHTML({id: 8, loginName: 'bob', chineseName: '鲍勃', passwordLogin: false}, {avatarConfigured: false, passwordLogin: false});
assert.ok(!sso.includes('id="profilePasswordForm"'), 'SSO accounts have no platform password form');
assert.ok(!sso.includes('id="profileAvatarRemove"'), 'system avatars cannot be removed');
assert.ok(sso.includes('GitHub 或单点登录'));
console.log('user profile helpers ok');
