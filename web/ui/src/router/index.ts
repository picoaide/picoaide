import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/Login.vue'),
    },
    {
      path: '/admin',
      name: 'admin',
      component: () => import('../layouts/AdminLayout.vue'),
      children: [
        { path: '', redirect: '/admin/dashboard' },
        { path: 'dashboard', name: 'admin-dashboard', component: () => import('../views/admin/Dashboard.vue') },
        { path: 'users', name: 'admin-users', component: () => import('../views/admin/Users.vue') },
        { path: 'groups', name: 'admin-groups', component: () => import('../views/admin/Groups.vue') },
        { path: 'teamspace', name: 'admin-teamspace', component: () => import('../views/admin/Teamspace.vue') },
        { path: 'skills', name: 'admin-skills', component: () => import('../views/admin/Skills.vue') },
        { path: 'channels', name: 'admin-channels', component: () => import('../views/admin/Channels.vue') },
        { path: 'models', name: 'admin-models', component: () => import('../views/admin/Models.vue') },
        { path: 'mcp-servers', name: 'admin-mcp-servers', component: () => import('../views/admin/MCPServers.vue') },
        { path: 'superadmins', name: 'admin-superadmins', component: () => import('../views/admin/Superadmins.vue') },
        { path: 'auth', name: 'admin-auth', component: () => import('../views/admin/Auth.vue') },
        { path: 'password', name: 'admin-password', component: () => import('../views/admin/Password.vue') },
        { path: 'tls', name: 'admin-tls', component: () => import('../views/admin/TLS.vue') },
      ],
    },
    {
      path: '/user',
      name: 'user',
      component: () => import('../layouts/UserLayout.vue'),
      children: [
        { path: '', redirect: '/user/chat' },
        { path: 'chat', name: 'user-chat', component: () => import('../views/user/Chat.vue') },
        { path: 'skills', name: 'user-skills', component: () => import('../views/user/Skills.vue') },
        { path: 'files', name: 'user-files', component: () => import('../views/user/Files.vue') },
        { path: 'channels', name: 'user-channels', component: () => import('../views/user/Channels.vue') },
        { path: 'email', name: 'user-email', component: () => import('../views/user/Email.vue') },
        { path: 'teamspace', name: 'user-teamspace', component: () => import('../views/user/Teamspace.vue') },
        { path: 'authorization', name: 'user-authorization', component: () => import('../views/user/Authorization.vue') },
        { path: 'cron', name: 'user-cron', component: () => import('../views/user/Cron.vue') },
        { path: 'password', name: 'user-password', component: () => import('../views/user/Password.vue') },
        { path: 'settings', name: 'user-settings', component: () => import('../views/user/Settings.vue') },
      ],
    },
    {
      path: '/',
      redirect: '/user/chat',
    },
  ],
})

router.beforeEach((to, _from, next) => {
  const token = localStorage.getItem('session')
  if (to.path !== '/login' && !token) {
    next('/login')
  } else {
    next()
  }
})

export default router
