'use strict';

var ByteFlood = Stapes.subclass({
    constructor: function(options){
        this.site = {
            title: 'ByteFlood Manager',
            brand: 'ByteFlood',
        };

        this.extend(options || {});

        rivets.configure({
             templateDelimiters: ['{{', '}}'],
        });
    },

    // Setup page bindings and routes and perform the initial data load, then
    // show everything.
    //
    run: function(){
        this._boundFullPage = rivets.bind($('html'), this);

        this._router = Router({
            '/': function(){
                this.chpage('index', this);
            }.bind(this),
        });


        this.chpage('index');
        this._router.init();
        $('body').removeAttr('style');
        $('title').text(this.site.title);
    },

    // Attempt to load the content of the named template into the container pointed at by viewTarget
    //
    chpage: function(template, controller) {
        $(this.viewTarget).load('/views/'+template+'.html', null, function(content, status){
            switch(status){
            case 'success':
                if(controller){
                    if(this._boundContent){
                        rivets.unbind(this._boundContent);
                    }

                }else{
                    controller = this;
                }

                rivets.bind($(this.viewTarget), controller);

                break;

            default:
                $(this.viewTarget).load('/views/error.html', null, function(content, status){
                    if(status == 'error'){
                        $(this.viewTarget).text('Failed to load template "'+template+'". Additionally, an error page is not configured.');
                    }
                });
            }
        }.bind(this));
    },
});

$(document).ready(function(){
    window.application = new ByteFlood({
        viewTarget: '#content',
    });

    application.run();
});
