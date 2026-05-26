export async function init(ctx) {
  const { Api, esc } = ctx;

  const [usersData, skillsData] = await Promise.all([
    Api.get('/api/admin/users').catch(() => ({ users: [] })),
    Api.get('/api/admin/skills').catch(() => ({ skills: [] })),
  ]);

  const users = (usersData.users || []).filter(u => u.role !== 'superadmin');

  ctx.$('#stat-users').textContent = users.length;
  ctx.$('#stat-skills').textContent = (skillsData.skills || []).length;

  const tbody = ctx.$('#dash-users');
  tbody.innerHTML = '';
  for (const u of users) {
    tbody.innerHTML += '<tr><td><strong>' + esc(u.username) + '</strong></td></tr>';
  }
}
