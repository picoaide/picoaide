export async function init(ctx) {
  const { Api, esc } = ctx;

  const [usersData, skillsData] = await Promise.all([
    Api.get('/api/admin/users').catch(() => ({ users: [] })),
    Api.get('/api/admin/skills').catch(() => ({ skills: [] })),
  ]);

  const users = usersData.users || [];
  const running = users.filter(u => u.status && u.status.startsWith('Up')).length;
  const noImage = users.filter(u => !u.image_ready).length;

  ctx.$('#stat-users').textContent = users.length;
  ctx.$('#stat-running').textContent = running;
  ctx.$('#stat-stopped').textContent = users.length - running;
  ctx.$('#stat-skills').textContent = (skillsData.skills || []).length;

  const tbody = ctx.$('#dash-users');
  tbody.innerHTML = '';
  for (const u of users) {
    const statusCls = u.status?.startsWith('Up') ? 'badge-ok' : 'badge-muted';
    const imgBadge = u.image_ready
      ? ''
      : ' <span class="badge badge-danger">未拉取</span>';
    tbody.innerHTML += '<tr><td><strong>' + esc(u.username) + '</strong></td><td><span class="badge ' + statusCls + '">' + esc(u.status) + '</span></td><td>' + esc(u.image_tag) + imgBadge + '</td><td>' + esc(u.ip || '-') + '</td></tr>';
  }

  if (noImage > 0) {
    const tip = ctx.$('#dash-image-tip');
    if (tip) tip.classList.remove('hidden');
  }
}
