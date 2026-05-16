// Counter
var count = 0;
var countEl = document.getElementById('count');
document.getElementById('increment').addEventListener('click', function () {
  count++;
  countEl.textContent = count;
  countEl.style.color = count > 0 ? '#27ae60' : count < 0 ? '#e74c3c' : '#1a1a2e';
});
document.getElementById('decrement').addEventListener('click', function () {
  count--;
  countEl.textContent = count;
  countEl.style.color = count > 0 ? '#27ae60' : count < 0 ? '#e74c3c' : '#1a1a2e';
});

// Form
document.getElementById('demo-form').addEventListener('submit', function (e) {
  e.preventDefault();
  var name = document.getElementById('name-input').value || 'World';
  var color = document.getElementById('color-select').value || 'nothing';
  var output = document.getElementById('form-output');
  output.textContent = 'Hello, ' + name + '! Your favorite color is ' + color + '.';
  output.hidden = false;
});

// Animation toggle
var bouncer = document.getElementById('bouncer');
document.getElementById('toggle-animation').addEventListener('click', function () {
  bouncer.classList.toggle('paused');
  this.textContent = bouncer.classList.contains('paused') ? 'Resume Animation' : 'Toggle Animation';
});

// Dynamic list
var list = document.getElementById('item-list');
var input = document.getElementById('new-item');
document.getElementById('add-item').addEventListener('click', function () {
  var text = input.value.trim();
  if (!text) return;
  var li = document.createElement('li');
  var span = document.createElement('span');
  span.textContent = text;
  var btn = document.createElement('button');
  btn.textContent = 'Remove';
  btn.addEventListener('click', function () { li.remove(); });
  li.appendChild(span);
  li.appendChild(btn);
  list.appendChild(li);
  input.value = '';
});

input.addEventListener('keydown', function (e) {
  if (e.key === 'Enter') {
    e.preventDefault();
    document.getElementById('add-item').click();
  }
});
