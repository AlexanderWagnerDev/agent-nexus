import { api } from '../api.js';
import { ensureStepUp } from '../auth.js';
import { state } from '../state.js';
import { closeModal, modal, toast } from '../ui.js';
import { t } from '../i18n.js';
import { esc } from '../ui-format.js';

export async function shareHostDialog(host) {
  try {
    await ensureStepUp();
  } catch {
    return;
  }
  const [users, grants] = await Promise.all([api('/users'), api(`/hosts/${host.id}/grants`)]);
  const byUser = new Map(grants.grants.map((g) => [g.user_id, g.permissions.split(',')]));
  const permissions = ['view','read_transcript','resume','create','fork','terminal_input','approve','manage'];
  modal(`
    <h2>${t('Share')} ${esc(host.name)}</h2>
    <p class="hint">${t('The hub grant is only one gate; the host policy still decides which agents, roots and actions are allowed.')}</p>
    <div class="form">
      <label class="label">${t('User')}<select class="select" id="share-user">${users.users.map((u) => `<option value="${esc(u.id)}">${esc(u.username)}${u.id === state.me.id ? ' (you)' : ''}</option>`).join('')}</select></label>
      <div class="grid cols-2">${permissions.map((p) => `<label class="split small"><input type="checkbox" data-perm="${p}"> ${p.replaceAll('_',' ')}</label>`).join('')}</div>
      <div class="modal-actions">
        <button class="btn danger" type="button" id="remove-grant">${t('Remove access')}</button>
        <button class="btn" type="button" data-close>${t('Cancel')}</button>
        <button class="btn primary" type="button" id="save-grant">${t('Save grant')}</button>
      </div>
    </div>`);
  const select = document.getElementById('share-user');
  const setChecks = () => {
    const current = byUser.get(select.value) || [];
    document.querySelectorAll('[data-perm]').forEach((cb) => { cb.checked = current.includes(cb.dataset.perm); });
  };
  select.addEventListener('change', setChecks);
  setChecks();
  document.getElementById('save-grant').addEventListener('click', async () => {
    const selected = [...document.querySelectorAll('[data-perm]:checked')].map((cb) => cb.dataset.perm);
    try {
      await api(`/hosts/${host.id}/grants/${select.value}`, { method: 'PUT', body: { permissions: selected } });
      toast(t('Grant saved'));
      closeModal();
    } catch (err) { toast(err.message, true); }
  });
  document.getElementById('remove-grant').addEventListener('click', async () => {
    try {
      await api(`/hosts/${host.id}/grants/${select.value}`, { method: 'DELETE' });
      toast('Grant removed');
      closeModal();
    } catch (err) { toast(err.message, true); }
  });
}
