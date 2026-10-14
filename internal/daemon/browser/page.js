// Follow the Reviewer: on each event from the review's change stream, fetch
// the stop they are at and swap it in. Moving to another stop starts it at the
// top; a redraw of the same one keeps the scroll position.
(function () {
  var body = document.body;
  var main = document.getElementById("stop");
  var connection = document.getElementById("connection");

  function keyOf(root) {
    var stop = root.querySelector(".stop");
    return stop ? stop.getAttribute("data-key") : "";
  }

  function say(text) {
    connection.textContent = text;
    connection.hidden = text === "";
  }

  function redraw() {
    fetch(body.getAttribute("data-stop-url"), { cache: "no-store" })
      .then(function (response) {
        if (response.status === 404) {
          throw new Error("dbn is no longer holding this review.");
        }
        if (!response.ok) {
          throw new Error("dbn answered " + response.status + ".");
        }
        return response.text();
      })
      .then(function (html) {
        var was = keyOf(main);
        main.innerHTML = html;
        if (keyOf(main) !== was) {
          window.scrollTo(0, 0);
        }
        say("");
      })
      .catch(function (error) {
        say(error.message);
      });
  }

  var events = new EventSource(body.getAttribute("data-events-url"));
  events.addEventListener("redraw", redraw);
  // Opening again after losing dbn may have missed moves, so read what is
  // current.
  events.addEventListener("open", redraw);
  events.addEventListener("error", function () {
    say("Lost dbn. Reconnecting…");
  });
})();
