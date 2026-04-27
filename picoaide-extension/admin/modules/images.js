export async function init(ctx) {
  const { Api, esc, showMsg, $ } = ctx;

  await loadLocalImages();
  loadRegistryTags();

  $('#images-refresh-registry')?.addEventListener('click', loadRegistryTags);

  $('#pull-modal-close')?.addEventListener('click', closePullModal);
  $('#pull-close-btn')?.addEventListener('click', closePullModal);

  async function loadLocalImages() {
    const data = await Api.get('/api/admin/images').catch(() => ({ images: [] }));
    const tbody = $('#images-local');
    tbody.innerHTML = '';
    const images = data.images || [];
    if (images.length === 0) {
      $('#images-local-empty')?.classList.remove('hidden');
      return;
    }
    $('#images-local-empty')?.classList.add('hidden');
    for (const img of images) {
      const tags = (img.repo_tags || []).join(', ') || '(无标签)';
      tbody.innerHTML += '<tr><td style="font-family:monospace;font-size:13px">' + esc(tags) + '</td><td>' + esc(img.repo_tags?.[0]?.split(':')[1] || '-') + '</td><td>' + esc(img.size_str) + '</td></tr>';
    }
  }

  async function loadRegistryTags() {
    const data = await Api.get('/api/admin/images/registry').catch(() => ({ tags: [] }));
    const tbody = $('#images-registry');
    tbody.innerHTML = '';
    const tags = data.tags || [];
    if (tags.length === 0) {
      $('#images-registry-empty')?.classList.remove('hidden');
      return;
    }
    $('#images-registry-empty')?.classList.add('hidden');
    for (const tag of tags) {
      const tr = document.createElement('tr');
      tr.innerHTML = '<td style="font-family:monospace">' + esc(tag) + '</td><td><button class="btn btn-sm btn-primary" data-tag="' + esc(tag) + '">拉取</button></td>';
      tbody.appendChild(tr);
    }
    tbody.querySelectorAll('[data-tag]').forEach(btn => {
      btn.addEventListener('click', () => pullImage(btn.dataset.tag));
    });
  }

  async function pullImage(tag) {
    const modal = $('#image-pull-modal');
    const progress = $('#pull-progress');
    const nameEl = $('#pull-image-name');

    const serverUrl = localStorage.getItem('picoaide-server') || '';
    const imageRef = nameEl.textContent = serverUrl ? '' : '';
    // 构建镜像引用
    const cfgResp = await Api.get('/api/config').catch(() => null);
    let imageName = 'ghcr.io/picoaide/picoaide';
    if (cfgResp?.image?.name) {
      imageName = cfgResp.image.name;
    }
    const fullRef = imageName + ':' + tag;
    nameEl.textContent = fullRef;
    progress.innerHTML = '';
    modal.classList.remove('hidden');

    // 使用 EventSource 进行 SSE
    const csrf = await getCSRF();
    const formData = new FormData();
    formData.append('image', fullRef);
    formData.append('csrf_token', csrf);

    try {
      const serverBase = localStorage.getItem('picoaide-server') || window.location.origin;
      const response = await fetch(serverBase + '/api/admin/images/pull', {
        method: 'POST',
        body: formData,
        credentials: 'include',
      });

      if (!response.ok) {
        progress.innerHTML += '<div style="color:#e74c3c">拉取失败: ' + response.status + '</div>';
        return;
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });

        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const data = line.slice(6);
            try {
              const obj = JSON.parse(data);
              if (obj.status === 'done') {
                progress.innerHTML += '<div style="color:#2ecc71">拉取完成!</div>';
                await loadLocalImages();
                break;
              }
              if (obj.status) {
                progress.innerHTML += '<div>' + esc(obj.status) + (obj.progress ? ' ' + esc(obj.progress) : '') + '</div>';
              }
            } catch {
              progress.innerHTML += '<div>' + esc(data) + '</div>';
            }
            progress.scrollTop = progress.scrollHeight;
          }
        }
      }
    } catch (err) {
      progress.innerHTML += '<div style="color:#e74c3c">错误: ' + esc(err.message) + '</div>';
    }
  }

  async function getCSRF() {
    const data = await Api.get('/api/csrf');
    return data.csrf_token || '';
  }

  function closePullModal() {
    $('#image-pull-modal')?.classList.add('hidden');
  }
}
