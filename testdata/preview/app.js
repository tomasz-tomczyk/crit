document.addEventListener("DOMContentLoaded", function () {
  var button = document.getElementById("action");
  var status = document.getElementById("status");
  if (!button || !status) {
    return;
  }
  button.addEventListener("click", function () {
    status.textContent = status.textContent === "idle" ? "active" : "idle";
  });
});
