import { api } from '../api.js';
import { renderShell, setPage } from '../shell.js';
import { state } from '../state.js';
import { badge, toast } from '../ui.js';
import { t } from '../i18n.js';
import { esc, fmtTime, shortId } from '../ui-format.js';

export async function pageUsers() {
  const pageGen = state.pageGen;
  renderShell('users');
  const view = setPage(t('Users'), t('Hub roles and per-host permissions. Per-host grants are the unit of least privilege; host policy is still enforced separately.'));
  const data = await api('/users');
  if (pageGen !== state.pageGen) return;
  const isOwner = state.me.role === 'owner';
  view.innerHTML = `
    <div class="table-wrap">
      <table><thead><tr><th>${t('User')}</th><th>${t('Role')}</th><th>${t('Created')}</th></tr></thead><tbody>
      ${data.users.map((u) => `<tr>
        <td><b>${esc(u.username)}</b>${u.id === state.me.id ? ` <span class="badge online">${t('you')}</span>` : ''}<div class="mono-small">${esc(shortId(u.id))}</div></td>
        <td>${isOwner && u.id !== state.me.id ? `<select class="select" data-role-user="${esc(u.id)}">${['admin','member','viewer'].map((r) => `<option ${u.role === r ? 'selected' : ''}>${r}</option>`).join('')}</select>` : esc(t(u.role) || '—')}</td>
        <td>${esc(fmtTime(u.created_at))}</td>
      </tr>`).join('')}</tbody></table>
    </div>
    ${isOwner ? `<div class="card mt-3">
      <h2 class="mt-0">${t('Add a user')}</h2>
      <p class="hint">${t('The new user signs in with this password and can change roles only through an owner. TOTP can be enrolled after their first login.')}</p>
      <form class="form" id="create-user-form">
        <div class="form-row">
          <label class="label">${t('Username')}<input class="input" name="username" required minlength="3"></label>
          <label class="label">${t('Password')}<input class="input" name="password" type="password" required minlength="10"></label>
          <label class="label">${t('Role')}<select class="select" name="role"><option value="member">${t('member')}</option><option value="viewer">${t('viewer')}</option><option value="admin">${t('admin')}</option></select></label>
        </div>
        <div><button class="btn primary" type="submit">${t('Create user')}</button></div>
      </form>
    </div>` : ''}`;
  view.querySelectorAll('[data-role-user]').forEach((sel) => sel.addEventListener('change', async () => {
    try {
      await api(`/users/${sel.dataset.roleUser}`, { method: 'PATCH', body: { role: sel.value } });
      toast(t('Role updated'));
    } catch (err) { toast(err.message, true); pageUsers(); }
  }));
  document.getElementById('create-user-form')?.addEventListener('submit', async (event) => {
    event.preventDefault();
    const form = new FormData(event.target);
    try {
      await api('/users', { method: 'POST', body: {
        username: form.get('username'), password: form.get('password'), role: form.get('role'),
      } });
      toast(t('User created'));
      pageUsers();
    } catch (err) { toast(err.message, true); }
  });
}
